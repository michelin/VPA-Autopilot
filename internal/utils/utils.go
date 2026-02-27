package utils

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"

	"github.com/michelin/vpa-autopilot/internal/config"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

func IsResourceIgnored(namespace *corev1.Namespace) bool {
	// Check if the namespace is excluded
	if slices.Contains(config.ExcludedNamespaces, namespace.Name) {
		return true
	}
	// Check the namespace labels
	val, ok := namespace.Labels[config.ExcludedNamespaceLabelKey]
	if ok && val == config.ExcludedNamespaceLabelValue {
		return true
	}
	return false
}

// generation a VPA for a workload (statefulset or deployment)
//   - return the VPA to create and nil if the operation was successful
//     or nil and an error if the operation failed
//
// TODO: stop using originalWorkloadCPURequest when https://github.com/michelin/VPA-Autopilot/issues/30 is done, as VPA impact would not this annotation anymore
func GenerateAutomaticVPA(workloadGVK schema.GroupVersionKind, workloadMetadata metav1.ObjectMeta, originalWorkloadCPURequest int64) (*vpav1.VerticalPodAutoscaler, error) {
	// Generating the automatic VPA name
	// Hashing the workload name to provide unique identifier to the automatic VPA (10 chars)
	workloadHash := fnv.New32a()
	workloadHash.Write([]byte(workloadMetadata.Name))
	suffix := fmt.Sprint(workloadHash.Sum32())
	completeName := config.VpaNamePrefix

	if len(workloadMetadata.Name) > 253-len(config.VpaNamePrefix)-len(suffix) {
		// If the workload name is too long to be concatenated directly, truncate it and add sha to avoid name collision
		completeName += workloadMetadata.Name[:253-len(config.VpaNamePrefix)-len(suffix)] + suffix
	} else {
		// If the workload name is reasonable, concatenate it directly
		completeName += workloadMetadata.Name
	}
	controllerFlag := true

	controlledCpuValues := vpav1.ContainerControlledValuesRequestsOnly
	if config.TargetLimits {
		controlledCpuValues = vpav1.ContainerControlledValuesRequestsAndLimits
	}
	ownerAPIVersion, ownerKind := workloadGVK.ToAPIVersionAndKind()
	vpa := &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      completeName,
			Namespace: workloadMetadata.Namespace,
			Annotations: map[string]string{
				"vpa-autopilot.michelin.com/original-requests-sum": strconv.FormatInt(originalWorkloadCPURequest, 10),
			},
			Labels: map[string]string{
				config.VpaLabelKey: config.VpaLabelValue,
			},
			// Set owner reference to the targeted workload to facilitate deletion
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: ownerAPIVersion,
					Kind:       ownerKind,
					Name:       workloadMetadata.Name,
					UID:        workloadMetadata.UID,
					Controller: &controllerFlag,
				},
			},
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: ownerAPIVersion,
				Kind:       ownerKind,
				Name:       workloadMetadata.Name,
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &config.VpaBehaviourTyped,
			},
			// Target only CPU for all containers of the pods
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						ControlledResources: &[]corev1.ResourceName{
							corev1.ResourceCPU,
						},
						ControlledValues: &controlledCpuValues,
					},
				},
			},
		},
	}
	return vpa, nil
}

func CreateOrUpdateVPA(ctx context.Context, client client.Client, newVPA *vpav1.VerticalPodAutoscaler) error {
	// try to get the automatic VPA associated to the workload to determine if the automatic VPA needs to be created or updated
	existingVPA := newVPA.DeepCopy()
	errGet := client.Get(ctx, types.NamespacedName{Namespace: newVPA.Namespace, Name: newVPA.Name}, existingVPA)
	if errGet == nil {
		// The automatic VPA exists, let's update it with the desired configuration
		newVPA.ResourceVersion = existingVPA.ResourceVersion
		errUpdate := client.Update(ctx, newVPA)
		if errUpdate != nil {
			return errUpdate
		}
		return nil
	} else if errors.IsNotFound(errGet) {
		// The automatic VPA does not exists yet, let's create it
		err := client.Create(ctx, newVPA)
		if err != nil {
			return err
		}
		return nil
	} else {
		return errGet
	}
}

// Find all VPAs matching the workload defined in the parameters
//   - returns a list with all the VPAs that were matched
func FindMatchingVPA(ctx context.Context, k8sclient client.Client, targetWorkloadType string, targetWorkloadName string, targetWorkloadNamespace string) []*vpav1.VerticalPodAutoscaler {
	vpaList := &vpav1.VerticalPodAutoscalerList{}
	_ = k8sclient.List(ctx, vpaList, &client.ListOptions{Namespace: targetWorkloadNamespace})
	matchingList := make([]*vpav1.VerticalPodAutoscaler, 0)
	for _, vpa := range vpaList.Items {
		if strings.EqualFold(vpa.Spec.TargetRef.Kind, targetWorkloadType) && vpa.Spec.TargetRef.Name == targetWorkloadName {
			matchingList = append(matchingList, &vpa)
		}
	}
	return matchingList
}

// Return relevant info on the target of the HPA for the controllers.
//   - targetGVK: the GroupVersionKind of the HPA target
//   - targetMetatadata: the metadata of the HPA target
//   - originalWorkloadCPURequest: the sum of the CPU request of the target pods before any modification by the controller (used for VPA generation)
//   - error: in case of any error during the retrieval of the target information, it is returned here
//
// TODO: stop returning originalWorkloadCPURequest when https://github.com/michelin/VPA-Autopilot/issues/30 is done, as VPA impact would not this annotation anymore
func GetHPATargetInfo(ctx context.Context, k8sclient client.Client, hpaTarget *autoscalingv2.CrossVersionObjectReference, namespace string) (schema.GroupVersionKind, metav1.ObjectMeta, int64, error) {
	var requestCpuSum int64 = 0
	var targetGVK schema.GroupVersionKind = schema.GroupVersionKind{}
	var targetMetadata metav1.ObjectMeta = metav1.ObjectMeta{}
	var err error
	if hpaTarget == nil {
		return targetGVK, targetMetadata, -1, fmt.Errorf("HPA target is nil")
	}
	switch strings.ToLower(hpaTarget.Kind) { //We have to differentiate the cases due to typing difference and potentially different structure if other types are supported in the future
	case "deployment":
		targetDeployment := appsv1.Deployment{}
		err = k8sclient.Get(ctx, client.ObjectKey{Name: hpaTarget.Name, Namespace: namespace}, &targetDeployment)
		if err != nil {
			break
		}
		// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
		// TODO: remove this when https://github.com/michelin/VPA-Autopilot/issues/30 is done
		for _, container := range targetDeployment.Spec.Template.Spec.Containers {
			requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
		}
		targetGVK = targetDeployment.GroupVersionKind()
		targetMetadata = targetDeployment.ObjectMeta
	case "statefulset":
		targetStatefulSet := appsv1.StatefulSet{}
		err = k8sclient.Get(ctx, client.ObjectKey{Name: hpaTarget.Name, Namespace: namespace}, &targetStatefulSet)
		if err != nil {
			break
		}
		// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
		// TODO: remove this when https://github.com/michelin/VPA-Autopilot/issues/30 is done
		for _, container := range targetStatefulSet.Spec.Template.Spec.Containers {
			requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
		}
		targetGVK = targetStatefulSet.GroupVersionKind()
		targetMetadata = targetStatefulSet.ObjectMeta
	default:
		return targetGVK, targetMetadata, -1, fmt.Errorf("unsupported target workload type: %s", hpaTarget.Kind)
	}
	return targetGVK, targetMetadata, requestCpuSum, err
}

// Return relevant info on the target of the VPA for the controllers.
//   - targetGVK: the GroupVersionKind of the VPA target
//   - targetMetatadata: the metadata of the VPA target
//   - originalWorkloadCPURequest: the sum of the CPU request of the target pods before any modification by the controller (used for VPA generation)
//   - error: in case of any error during the retrieval of the target information, it is returned here
//
// TODO: stop returning originalWorkloadCPURequest when https://github.com/michelin/VPA-Autopilot/issues/30 is done, as VPA impact would not this annotation anymore
func GetVPATargetInfo(ctx context.Context, k8sclient client.Client, vpaTarget *v1.CrossVersionObjectReference, namespace string) (schema.GroupVersionKind, metav1.ObjectMeta, int64, error) {
	var requestCpuSum int64 = 0
	var targetGVK schema.GroupVersionKind = schema.GroupVersionKind{}
	var targetMetadata metav1.ObjectMeta = metav1.ObjectMeta{}
	var err error
	if vpaTarget == nil {
		return targetGVK, targetMetadata, -1, fmt.Errorf("VPA target is nil")
	}
	switch strings.ToLower(vpaTarget.Kind) { //We have to differentiate the cases due to typing difference and potentially different structure if other types are supported in the future
	case "deployment":
		targetDeployment := appsv1.Deployment{}
		err = k8sclient.Get(ctx, client.ObjectKey{Name: vpaTarget.Name, Namespace: namespace}, &targetDeployment)
		if err != nil {
			break
		}
		// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
		// TODO: remove this when https://github.com/michelin/VPA-Autopilot/issues/30 is done
		for _, container := range targetDeployment.Spec.Template.Spec.Containers {
			requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
		}
		targetGVK = targetDeployment.GroupVersionKind()
		targetMetadata = targetDeployment.ObjectMeta
	case "statefulset":
		targetStatefulSet := appsv1.StatefulSet{}
		err = k8sclient.Get(ctx, client.ObjectKey{Name: vpaTarget.Name, Namespace: namespace}, &targetStatefulSet)
		if err != nil {
			break
		}
		// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
		// TODO: remove this when https://github.com/michelin/VPA-Autopilot/issues/30 is done
		for _, container := range targetStatefulSet.Spec.Template.Spec.Containers {
			requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
		}
		targetGVK = targetStatefulSet.GroupVersionKind()
		targetMetadata = targetStatefulSet.ObjectMeta
	default:
		return targetGVK, targetMetadata, -1, fmt.Errorf("unsupported target workload type: %s", vpaTarget.Kind)
	}
	return targetGVK, targetMetadata, requestCpuSum, err
}
