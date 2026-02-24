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

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/utils"

	autoscaling "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// File containing all the elements shared by the vpa_auto_* controllers, to avoid code duplication
// It, itself, does not contain any controller

// Common part of reconcilliation between all workload types that get a VPA
// TODO: stop passing originalWorkloadCPURequest when https://github.com/michelin/VPA-Autopilot/issues/30 is done, as VPA impact would not this annotation anymore
func commonReconcile(ctx context.Context, r client.Client, workloadGVK schema.GroupVersionKind, workloadMetadata metav1.ObjectMeta, originalWorkloadCPURequest int64) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Before anything, check if a HPA or another VPA already targets the workload
	vpaPresent := false
	blockingVPAName := ""
	// List all VPAs in the workload namespace that target the workload
	vpaList := utils.FindMatchingVPA(ctx, r, workloadGVK.Kind, workloadMetadata.Name, workloadMetadata.Namespace)

	// Check if one of them is not managed by the controller
	for _, vpa := range vpaList {
		if len(vpa.OwnerReferences) == 0 {
			vpaPresent = true
			blockingVPAName = vpa.Name
		}
		for _, ownerRef := range vpa.OwnerReferences {
			if !*ownerRef.Controller || ownerRef.Kind != workloadGVK.Kind || ownerRef.Name != workloadMetadata.Name {
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
		logger.Info("Skipping workload because another VPA is already attached", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace, "vpa", blockingVPAName)
		return ctrl.Result{}, nil
	}

	hpaPresent := false
	blockingHPAName := ""
	// List all HPAs in the workload namespace that are not managed by the controller
	hpaList := &autoscaling.HorizontalPodAutoscalerList{}
	err := r.List(ctx, hpaList, &client.ListOptions{Namespace: workloadMetadata.Namespace})
	if err != nil {
		logger.Error(err, "Could not check if a HPA is attached to the workload", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace)
		return ctrl.Result{}, err
	}
	// Check if one of them targets the workload
	for _, hpa := range hpaList.Items {
		target := hpa.Spec.ScaleTargetRef
		if target.Kind == workloadGVK.Kind && target.APIVersion == workloadGVK.GroupVersion().String() && target.Name == workloadMetadata.Name {
			hpaPresent = true
			blockingHPAName = hpa.Name
			break
		}
	}

	if hpaPresent {
		logger.Info("Skipping workload because a HPA is already attached", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace, "hpa", blockingHPAName)
		return ctrl.Result{}, nil
	}

	newVPA, err := utils.GenerateAutomaticVPA(workloadGVK, workloadMetadata, originalWorkloadCPURequest)
	if err != nil {
		logger.Error(err, "VPA generation failed for workload", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace)
		return ctrl.Result{}, err
	}

	err = utils.CreateOrUpdateVPA(ctx, r, newVPA)
	if err != nil {
		logger.Error(err, "Cannot create or update automatic VPA", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace)
		return ctrl.Result{}, err
	}

	logger.Info("Created or updated automatic VPA of workload", "kind", workloadGVK.Kind, "name", workloadMetadata.Name, "namespace", workloadMetadata.Namespace)
	return ctrl.Result{}, nil
}

var workloadPredicate predicate.Funcs = predicate.Funcs{
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
