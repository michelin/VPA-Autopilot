/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/utils"

	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// AutoVPAReconciler reconciles a VerticalPodAutoscaler object
type AutoVPAReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	RequeueAfter time.Duration
}

// +kubebuilder:rbac:resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *AutoVPAReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Get deployment namespace to check if the namespace is ignored
	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: req.Namespace}, &namespace); err == nil {
		if utils.IsResourceIgnored(&namespace) {
			return ctrl.Result{}, nil
		}
	} else {
		logger.Error(err, "Failed to get namespace information", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	logger.Info("Begin reconcile automatic VPA for deployment", "name", req.Name, "namespace", req.Namespace)

	// Get the deployment complete object
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		// here, we could not get the deployment object
		if errors.IsNotFound(err) {
			// here, the deployment corresponding to the event being processed does not exist
			// --> the event is a removal: nothing to do as the automatic vpa will be deleted through ownerReferences
			// let's return an empty Result and no error to indicate that no
			// requeueing is necessary
			logger.Info("Deployment is deleted", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch deployment")
		// we got an error getting the Custer but this was not a NotFound error
		// let's return an error to indicate that the operation failed and that the
		// controller should requeue the request
		return ctrl.Result{}, err
	}

	// Here, the deployment was retrieved: the event is a creation or an update
	logger.Info("Handling VPA for deployment", "name", req.Name, "namespace", req.Namespace)

	// Before anything, check if a HPA or another VPA already targets the deployment
	vpaPresent := false
	blockingVPAName := ""
	// List all VPAs in the deployment namespace that target the deployment
	vpaList := utils.FindMatchingVPA(ctx, r.Client, req.NamespacedName)

	// Check if one of them is not managed by the controller
	for _, vpa := range vpaList {
		if len(vpa.OwnerReferences) == 0 {
			vpaPresent = true
			blockingVPAName = vpa.Name
		}
		for _, ownerRef := range vpa.OwnerReferences {
			if !*ownerRef.Controller || ownerRef.Kind != "Deployment" || ownerRef.Name != req.Name {
				vpaPresent = true
				blockingVPAName = vpa.Name
				break
			}
		}
		if vpaPresent {
			break
		}
	}

	if vpaPresent {
		logger.Info("Skipping deployment because another VPA is already attached", "name", req.Name, "namespace", req.Namespace, "vpa", blockingVPAName)
		return ctrl.Result{}, nil
	}

	hpaPresent := false
	blockingHPAName := ""
	// List all VPAs in the deployment namespace that are not managed by the controller
	hpaList := &autoscaling.HorizontalPodAutoscalerList{}
	err := r.Client.List(ctx, hpaList, &client.ListOptions{Namespace: req.Namespace})
	if err != nil {
		logger.Error(err, "Could not check if a HPA is attached to the deployment", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	// Check if one of them targets the deployment
	for _, hpa := range hpaList.Items {
		target := hpa.Spec.ScaleTargetRef
		if target.Kind == deployment.Kind && target.APIVersion == deployment.APIVersion && target.Name == deployment.Name {
			hpaPresent = true
			blockingHPAName = hpa.Name
			break
		}
	}

	if hpaPresent {
		logger.Info("Skipping deployment because a HPA is already attached", "name", req.Name, "namespace", req.Namespace, "hpa", blockingHPAName)
		return ctrl.Result{}, nil
	}

	newVPA, err := utils.GenerateAutomaticVPA(&deployment)
	if err != nil {
		logger.Error(err, "VPA generation failed for deployment", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	err = utils.CreateOrUpdateVPA(ctx, r.Client, newVPA)
	if err != nil {
		logger.Error(err, "Cannot create or update automatic VPA", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	logger.Info("Created or updated automatic VPA of deployment", "name", req.Name, "namespace", req.Namespace)
	return ctrl.Result{}, nil
}

var deploymentPredicate predicate.Funcs = predicate.Funcs{
	// No reconcilliation on update
	UpdateFunc: func(e event.UpdateEvent) bool {
		return false
	},
	// Triggers reconcilliation on create events
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	// No need to reconcile on delete events thanks to the ownerRefenrence set in the automatic VPA
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},

	// Not used
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

var vpaPredicate predicate.Funcs = predicate.Funcs{
	// Do reconcilliation on update when:
	//    - specs are changed and the VPA is managed by the controller
	//    - the identifying label changed
	UpdateFunc: func(e event.UpdateEvent) bool {
		vpaNew := e.ObjectNew
		vpaOld := e.ObjectOld

		isSpecChanged := vpaOld.GetGeneration() != vpaNew.GetGeneration()

		matchLabelOld := false
		matchLabelNew := false

		if value, present := vpaOld.GetLabels()[config.VpaLabelKey]; present {
			matchLabelOld = (value == config.VpaLabelValue)
		}
		if value, present := vpaNew.GetLabels()[config.VpaLabelKey]; present {
			matchLabelNew = (value == config.VpaLabelValue)
		}

		return (isSpecChanged && (matchLabelOld || matchLabelNew)) || (matchLabelOld != matchLabelNew)
	},

	// Do not watch create events
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},

	// Watch delete events for vpa that are managed by the controller
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},

	// Not used
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutoVPAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}, builder.WithPredicates(deploymentPredicate)).
		Owns(&vpav1.VerticalPodAutoscaler{}, builder.WithPredicates(vpaPredicate)).
		Named("auto_vpa_go").
		Complete(r)
}
