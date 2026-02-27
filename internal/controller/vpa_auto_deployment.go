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

	"github.com/michelin/vpa-autopilot/internal/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AutoVPADeploymentReconciler reconciles a VerticalPodAutoscaler object when a deployment is created
type AutoVPADeploymentReconciler struct {
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
func (r *AutoVPADeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		// we got an error getting the Deployment but this was not a NotFound error
		// let's return an error to indicate that the operation failed and that the
		// controller should requeue the request
		return ctrl.Result{}, err
	}

	logger.Info("Handling VPA for deployment", "name", req.Name, "namespace", req.Namespace)

	// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
	// TODO: remove this when https://github.com/michelin/VPA-Autopilot/issues/30 is done
	var requestCpuSum int64
	requestCpuSum = 0
	for _, container := range deployment.Spec.Template.Spec.Containers {
		requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
	}

	// Calling the common reconcile function with the deployment information
	deploymentGVK, err := apiutil.GVKForObject(&deployment, r.Scheme)
	if err != nil {
		logger.Error(err, "Cannot get GVK for deployment", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	return commonReconcile(ctx, r.Client, deploymentGVK, deployment.ObjectMeta, requestCpuSum)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutoVPADeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}, builder.WithPredicates(workloadPredicate)).
		Owns(&vpav1.VerticalPodAutoscaler{}, builder.WithPredicates(vpaPredicate)).
		Named("vpa_auto_deployment").
		Complete(r)
}
