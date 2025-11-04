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
	"fmt"
	"reflect"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// HorizontalPodAutoscalerReconciler reconciles a HorizontalPodAutoscaler object
type HorizontalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers/status,verbs=get

// Variable that stores the active client HPA targets to use them during delete or update events
// Relies on the fact that autoscalingv1.CrossVersionObjectReference is the same as autoscalingv2.CrossVersionObjectReference
var clientHPACache map[types.NamespacedName]*autoscalingv2.CrossVersionObjectReference = make(map[types.NamespacedName]*autoscalingv2.CrossVersionObjectReference)

// Function used to get the targetRef of a HPA regardless if it is v1 or v2
func extractHPATarget(hpa interface{}) *autoscalingv2.CrossVersionObjectReference {
	switch t := hpa.(type) {
	case autoscalingv1.HorizontalPodAutoscaler:
		tmp := hpa.(autoscalingv1.HorizontalPodAutoscaler).Spec.ScaleTargetRef
		ret := autoscalingv2.CrossVersionObjectReference{
			Kind:       tmp.Kind,
			Name:       tmp.Name,
			APIVersion: tmp.APIVersion,
		}
		return &ret
	case autoscalingv2.HorizontalPodAutoscaler:
		ret := hpa.(autoscalingv2.HorizontalPodAutoscaler).Spec.ScaleTargetRef
		return &ret
	default:
		fmt.Printf("extractHPATarget invoked with unsupported type: '%T' (expected autoscalingv1.HorizontalPodAutoscaler or autoscalingv2.HorizontalPodAutoscaler)\n", t)
		return nil
	}
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *HorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	logger.Info("Begin handle client HPA", "name", req.Name, "namespace", req.Namespace)

	// Try to get the hpa specs
	var clientHPATarget *autoscalingv2.CrossVersionObjectReference = nil

	// Try with autoscaling v1...
	clientHPAv1 := autoscalingv1.HorizontalPodAutoscaler{}
	errV1 := r.Get(ctx, req.NamespacedName, &clientHPAv1)

	if errV1 != nil && !errors.IsNotFound(errV1) {
		logger.Error(errV1, "Could not get HPA v1 object", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, errV1
	} else if errV1 == nil {
		clientHPATarget = extractHPATarget(clientHPAv1)
	}
	// ... then with autoscaling v2
	clientHPAv2 := autoscalingv2.HorizontalPodAutoscaler{}
	errV2 := r.Get(ctx, req.NamespacedName, &clientHPAv2)

	if errV2 != nil && !errors.IsNotFound(errV2) {
		logger.Error(errV2, "Could not get HPA v2 object", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, errV2
	} else if errV2 == nil {
		clientHPATarget = extractHPATarget(clientHPAv2)
	}

	if errors.IsNotFound(errV1) && errors.IsNotFound(errV2) {
		// If both versions return Not Found, then the HPA was deleted
		logger.Info("Client HPA was deleted", "name", req.Name, "namespace", req.Namespace)
		// In this case, restore the automatic HPA with the help of cached data
		if cachedHPA, isInCache := clientHPACache[req.NamespacedName]; isInCache {
			targetDeployment := appsv1.Deployment{}
			err := r.Get(ctx, client.ObjectKey{Name: cachedHPA.Name, Namespace: req.Namespace}, &targetDeployment)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted deployment does not exists, do nothing
					logger.Info("Client HPA targeted non existing deployment, nothing to be done", "name", req.Name, "namespace", req.Namespace)
					delete(clientHPACache, req.NamespacedName)
					return ctrl.Result{}, nil
				} else {
					logger.Error(err, config.DeploymentGetError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}
			logger.Info("Creating automatic VPA from cached info", "name", req.Name, "namespace", req.Namespace)
			vpa, err := utils.GenerateAutomaticVPA(&targetDeployment)
			if err != nil {
				logger.Error(err, config.VPAGenerationError, "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, err
			}
			err = utils.CreateOrUpdateVPA(ctx, r.Client, vpa)
			if err != nil {
				logger.Error(err, "Could create or update automatic VPA", "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("Restored automatic VPA of deployment %s", targetDeployment.Name), "name", req.Name, "namespace", req.Namespace)
			// remove client HPA from cache now that operations are finished
			delete(clientHPACache, req.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			logger.Error(fmt.Errorf("Client HPA %s in namespace %s was not in cache.", req.Name, req.Namespace), "Missing cache data cannot create automatic VPA.", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil // Do not requeue to avoid infinite loop because the cache will never miraculously populate itself
		}
	} else {
		// Here, the client HPA was updated or created
		// FIXME: Ignore the HPA if it targets something different than a deployment for now, the controller should be reworked to handle other targets
		if clientHPATarget.Kind != "Deployment" {
			logger.Info("The HPA targets something else than a deployment. This is not supported yet!", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		if cachedHPA, isInCache := clientHPACache[req.NamespacedName]; isInCache {
			// If the client vpa was already in cache, then it means it was updated
			// Before anything, check that the updae is relevant for this controller (i.e. the target changed)
			if reflect.DeepEqual(cachedHPA, clientHPATarget) {
				// If the target stays the same, the update is not important and the controller can skip the event
				logger.Info("Client HPA update is irrelevant, skipping it", "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, nil
			}
			logger.Info("Client HPA was updated", "name", req.Name, "namespace", req.Namespace)
			// If in cache (i.e updated) create automatic VPA from cached info for old target if it exists
			targetDeploymentOld := appsv1.Deployment{}
			err := r.Get(ctx, client.ObjectKey{Name: cachedHPA.Name, Namespace: req.Namespace}, &targetDeploymentOld)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted deployment does not exists, do nothing
					logger.Info("Client VPA targeted non existing deployment, nothing to be done", "name", req.Name, "namespace", req.Namespace)
				} else {
					logger.Error(err, config.DeploymentGetError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				// old deployment specs were found, creating automatic VPA for it
				vpa, err := utils.GenerateAutomaticVPA(&targetDeploymentOld)
				if err != nil {
					logger.Error(err, config.VPAGenerationError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
				err = utils.CreateOrUpdateVPA(ctx, r.Client, vpa)
				if err != nil {
					logger.Error(err, "Could create or update automatic VPA", "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
				logger.Info(fmt.Sprintf("Restored automatic VPA of old target %s", targetDeploymentOld.Name), "name", req.Name, "namespace", req.Namespace)
			}
			// after restoring the old deployment automatic VPA, delete the automatic VPA of the deployment targeted by the new verion of the client HPA
			targetDeploymentNew := appsv1.Deployment{}
			err = r.Get(ctx, client.ObjectKey{Name: clientHPATarget.Name, Namespace: req.Namespace}, &targetDeploymentNew)
			fmt.Printf("Name: %s, Namespace: %s\n", clientHPATarget.Name, req.Namespace)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted deployment does not exists, do nothing
					logger.Info("Client VPA now targets non existing deployment, nothing to be done", "name", req.Name, "namespace", req.Namespace)
					clientHPACache[req.NamespacedName] = clientHPATarget
					return ctrl.Result{}, nil
				} else {
					logger.Error(err, config.DeploymentGetError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}
			vpa, err := utils.GenerateAutomaticVPA(&targetDeploymentNew)
			if err != nil {
				logger.Error(err, config.VPAGenerationError, "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, err
			}
			err = r.Delete(ctx, vpa)
			if err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Could not delete colliding automatic VPA", "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}
			logger.Info(fmt.Sprintf("Deleted automatic VPA of new target %s", targetDeploymentNew.Name), "name", req.Name, "namespace", req.Namespace)
			// Update the cache now that operations are finished
			clientHPACache[req.NamespacedName] = clientHPATarget
			return ctrl.Result{}, nil
		} else {
			// If not in cache (i.e just created), try to delete automatic VPA if it exists
			logger.Info("Client HPA was created", "name", req.Name, "namespace", req.Namespace)
			// Delete the automatic VPA of the deployment targeted by the new verion of the client HPA
			targetDeploymentNew := appsv1.Deployment{}
			err := r.Get(ctx, client.ObjectKey{Name: clientHPATarget.Name, Namespace: req.Namespace}, &targetDeploymentNew)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted deployment does not exists, do nothing
					logger.Info("Client HPA targets non existing deployment, nothing to be done", "name", req.Name, "namespace", req.Namespace)
					clientHPACache[req.NamespacedName] = clientHPATarget
					return ctrl.Result{}, nil
				} else {
					logger.Error(err, config.DeploymentGetError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}
			vpa, err := utils.GenerateAutomaticVPA(&targetDeploymentNew)
			if err != nil {
				logger.Error(err, config.VPAGenerationError, "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, err
			}
			err = r.Delete(ctx, vpa)
			if err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Could not delete colliding automatic VPA", "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}
			logger.Info(fmt.Sprintf("Deleted automatic VPA %s", vpa.Name), "name", req.Name, "namespace", req.Namespace)
			clientHPACache[req.NamespacedName] = clientHPATarget
		}
		return ctrl.Result{}, nil
	}
	// return ctrl.Result{}, fmt.Errorf("Should never reach this code!")
}

// Predicate for events of client's HPA
var clientHPAPredicate predicate.Funcs = predicate.Funcs{
	// Do reconcilliation on update when specs are changed
	UpdateFunc: func(e event.UpdateEvent) bool {
		return true
	},

	// Watch create events for hpa
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	// Watch delete events for hpa
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},

	// Not used
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *HorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&autoscalingv1.HorizontalPodAutoscaler{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(clientHPAPredicate),
		).
		Watches(&autoscalingv2.HorizontalPodAutoscaler{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(clientHPAPredicate),
		).
		Named("horizontalpodautoscaler").
		Complete(r)
}
