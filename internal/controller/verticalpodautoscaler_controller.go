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

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// VerticalPodAutoscalerReconciler reconciles a VerticalPodAutoscaler object
type VerticalPodAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers/finalizers,verbs=update

// Variable that stores the active client VPA objects to use them during delete or update events
var clientVPACache map[types.NamespacedName]vpav1.VerticalPodAutoscaler = make(map[types.NamespacedName]vpav1.VerticalPodAutoscaler)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *VerticalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Get vpa namespace to check if the namespace is ignored
	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: req.Namespace}, &namespace); err == nil {
		if utils.IsResourceIgnored(&namespace) {
			return ctrl.Result{}, nil
		}
	} else {
		logger.Error(err, "Failed to get namespace information", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	logger.Info("Begin handle client VPA", "name", req.Name, "namespace", req.Namespace)

	// Try to get the object specs
	var clientVPA vpav1.VerticalPodAutoscaler
	if errGet := r.Get(ctx, req.NamespacedName, &clientVPA); errGet != nil {
		// If error is NotFound, then the client VPA was deleted
		if errors.IsNotFound(errGet) {
			logger.Info("Client VPA was deleted", "name", req.Name, "namespace", req.Namespace)
			// Create automatic VPA using some of the cached vpa specs
			if cachedVPA, isInCache := clientVPACache[req.NamespacedName]; isInCache {
				logger.Info("Creating automatic VPA from cached info", "name", req.Name, "namespace", req.Namespace)
				targetGVK, targetMetadata, requestCpuSum, err := utils.GetVPATargetInfo(ctx, r.Client, cachedVPA.Spec.TargetRef, req.Namespace)
				if err != nil {
					if errors.IsNotFound(err) {
						// The targeted workload does not exists, do nothing
						logger.Info("Client VPA targeted non existing workload, nothing to be done", "name", req.Name, "namespace", req.Namespace)
						delete(clientVPACache, req.NamespacedName)
						return ctrl.Result{}, nil
					} else {
						logger.Error(err, config.WorkloadGetError, "name", req.Name, "namespace", req.Namespace)
						return ctrl.Result{}, err
					}
				}

				vpa, err := utils.GenerateAutomaticVPA(targetGVK, targetMetadata, requestCpuSum)
				if err != nil {
					logger.Error(err, config.VPAGenerationError, "kind", targetGVK.Kind, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}

				err = utils.CreateOrUpdateVPA(ctx, r.Client, vpa)
				if err != nil {
					logger.Error(err, "Could create or update automatic VPA", "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}

				logger.Info(fmt.Sprintf("Restored automatic VPA of %s %s", cachedVPA.Spec.TargetRef.Kind, cachedVPA.Spec.TargetRef.Name), "name", req.Name, "namespace", req.Namespace)
				// remove client vpa from cache now that operations are finished
				delete(clientVPACache, req.NamespacedName)
				return ctrl.Result{}, nil
			} else {
				logger.Error(fmt.Errorf("Client VPA %s in namespace %s was not in cache.", req.Name, req.Namespace), "Missing cache data cannot create automatic VPA.", "name", req.Name, "namespace", req.Namespace)
				return ctrl.Result{}, nil // Do not requeue to avoid infinite loop because the cache will never miraculously populate itself
			}
		}
	} else {
		// Here, the client VPA was updated or created
		if cachedVPA, isInCache := clientVPACache[req.NamespacedName]; isInCache {
			// If the client vpa was already in cache, then it means it was updated
			logger.Info("Client VPA was updated", "name", req.Name, "namespace", req.Namespace)
			// If in cache (i.e updated) create automatic VPA from cached info
			targetGVK, targetMetadata, requestCpuSum, err := utils.GetVPATargetInfo(ctx, r.Client, cachedVPA.Spec.TargetRef, req.Namespace)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted workload does not exists, do nothing
					logger.Info("Client VPA targeted non existing workload, nothing to be done", "name", req.Name, "namespace", req.Namespace)
				} else {
					logger.Error(err, config.WorkloadGetError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				vpa, err := utils.GenerateAutomaticVPA(targetGVK, targetMetadata, requestCpuSum)
				if err != nil {
					logger.Error(err, config.VPAGenerationError, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
				err = utils.CreateOrUpdateVPA(ctx, r.Client, vpa)
				if err != nil {
					logger.Error(err, "Could create or update automatic VPA", "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
				logger.Info(fmt.Sprintf("Restored automatic VPA of old target %s %s", targetGVK.Kind, targetMetadata.Name), "name", req.Name, "namespace", req.Namespace)
			}

			// after restoring the old workload's automatic vpa, delete the automatic VPA of the workload targeted by the new version of the client VPA
			targetGVK, targetMetadata, requestCpuSum, err = utils.GetVPATargetInfo(ctx, r.Client, clientVPA.Spec.TargetRef, req.Namespace)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted workload does not exists, do nothing
					logger.Info("Client VPA now targets non existing workload, nothing to be done", "name", req.Name, "namespace", req.Namespace)
					clientVPACache[req.NamespacedName] = clientVPA
					return ctrl.Result{}, nil
				} else {
					logger.Error(err, config.WorkloadGetError, "kind", clientVPA.Spec.TargetRef.Kind, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}

			vpa, err := utils.GenerateAutomaticVPA(targetGVK, targetMetadata, requestCpuSum)
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
			logger.Info(fmt.Sprintf("Deleted automatic VPA of new target %s %s", targetGVK.Kind, targetMetadata.Name), "name", req.Name, "namespace", req.Namespace)
			// Update the cache now that operations are finished
			clientVPACache[req.NamespacedName] = clientVPA
			return ctrl.Result{}, nil
		} else {
			// If not in cache (i.e just created), try to delete automatic vpa if it exists
			logger.Info("Client VPA was created", "name", req.Name, "namespace", req.Namespace)
			// Delete the automatic VPA of the workload targeted by the new version of the client VPA
			targetGVK, targetMetadata, requestCpuSum, err := utils.GetVPATargetInfo(ctx, r.Client, clientVPA.Spec.TargetRef, req.Namespace)
			if err != nil {
				if errors.IsNotFound(err) {
					// The targeted workload does not exists, do nothing
					logger.Info("Client VPA targets non existing workload, nothing to be done", "name", req.Name, "namespace", req.Namespace)
					clientVPACache[req.NamespacedName] = clientVPA
					return ctrl.Result{}, nil
				} else {
					logger.Error(err, config.WorkloadGetError, "kind", clientVPA.Spec.TargetRef.Kind, "name", req.Name, "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			}

			vpa, err := utils.GenerateAutomaticVPA(targetGVK, targetMetadata, requestCpuSum)
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
			clientVPACache[req.NamespacedName] = clientVPA
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, fmt.Errorf("Should never reach this code!")
}

// Predicate for events of client's VPA where an automatic VPA should be deleted
var clientVPAPredicate predicate.Funcs = predicate.Funcs{
	// Do reconcilliation on update when:
	//    - specs are changed and the VPA is NOT managed by the controller
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

		return isSpecChanged && (!matchLabelOld && !matchLabelNew)
	},

	// Watch create events for vpa that are NOT managed by the controller
	CreateFunc: func(e event.CreateEvent) bool {
		vpaLabels := e.Object.GetLabels()
		clientVPA := true
		if value, present := vpaLabels[config.VpaLabelKey]; present {
			if value == config.VpaLabelValue {
				clientVPA = false
			}
		}
		return clientVPA
	},

	// Watch delete events for vpa that are NOT managed by the controller
	DeleteFunc: func(e event.DeleteEvent) bool {
		vpaLabels := e.Object.GetLabels()
		clientVPA := true
		if value, present := vpaLabels[config.VpaLabelKey]; present {
			if value == config.VpaLabelValue {
				clientVPA = false
			}
		}
		return clientVPA
	},

	// Not used
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vpav1.VerticalPodAutoscaler{}, builder.WithPredicates(clientVPAPredicate)).
		Named("verticalpodautoscaler").
		Complete(r)
}
