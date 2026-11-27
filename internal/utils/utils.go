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

type supportedWorkloadTypes struct {
	ApiVersion string
	Kind       string
}

// List of kinds supported by the VPA Autopilot.
// It is required because depending on the kinds, the information on the original resource reservation is retrieved differently
// TODO: Review how this list is used when https://github.com/michelin/VPA-Autopilot/issues/30 is done
var ListKindSupported = []supportedWorkloadTypes{
	{ApiVersion: "apps/v1", Kind: "Deployment"},
	{ApiVersion: "apps/v1", Kind: "StatefulSet"},
}

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

// maxVPANameLength is the maximum length of a Kubernetes resource name (DNS subdomain)
const maxVPANameLength = 253

// AutomaticVPAName returns the name of the automatic VPA of a workload: <prefix><lowercase kind>-<workload name>.
// The kind is part of the name so that workloads of different kinds sharing the same name do not collide.
func AutomaticVPAName(workloadKind string, workloadName string) string {
	base := config.VpaNamePrefix + strings.ToLower(workloadKind) + "-"
	if len(base)+len(workloadName) <= maxVPANameLength {
		// If the workload name is reasonable, concatenate it directly
		return base + workloadName
	}

	// If the workload name is too long to be concatenated directly, truncate it and add a hash of kind and name to avoid name collision
	workloadHash := fnv.New32a()
	workloadHash.Write([]byte(workloadKind + "/" + workloadName))
	suffix := fmt.Sprint(workloadHash.Sum32())
	truncated := workloadName[:maxVPANameLength-len(base)-len(suffix)]
	// A name must end with an alphanumeric character before the suffix is appended
	truncated = strings.TrimRight(truncated, "-.")
	return base + truncated + suffix
}

// generation a VPA for a workload (statefulset or deployment)
//   - return the VPA to create and nil if the operation was successful
//     or nil and an error if the operation failed
//
// TODO: stop using originalWorkloadCPURequest when https://github.com/michelin/VPA-Autopilot/issues/30 is done, as VPA impact would not this annotation anymore
func GenerateAutomaticVPA(workloadGVK schema.GroupVersionKind, workloadMetadata metav1.ObjectMeta, originalWorkloadCPURequest int64) (*vpav1.VerticalPodAutoscaler, error) {
	completeName := AutomaticVPAName(workloadGVK.Kind, workloadMetadata.Name)
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
		// Never take over a VPA that is not controlled by the same workload
		if !isControlledBySameWorkload(existingVPA, newVPA) {
			return fmt.Errorf("VPA %s/%s already exists and is not controlled by the same workload", newVPA.Namespace, newVPA.Name)
		}
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

// IsControlledByWorkload returns true if the VPA has a controller owner reference to the workload of the given kind and name
func IsControlledByWorkload(vpa *vpav1.VerticalPodAutoscaler, workloadKind string, workloadName string) bool {
	for _, ownerRef := range vpa.OwnerReferences {
		if ownerRef.Controller != nil && *ownerRef.Controller && strings.EqualFold(ownerRef.Kind, workloadKind) && ownerRef.Name == workloadName {
			return true
		}
	}
	return false
}

func isControlledBySameWorkload(existingVPA *vpav1.VerticalPodAutoscaler, newVPA *vpav1.VerticalPodAutoscaler) bool {
	for _, ownerRef := range newVPA.OwnerReferences {
		if ownerRef.Controller != nil && *ownerRef.Controller && IsControlledByWorkload(existingVPA, ownerRef.Kind, ownerRef.Name) {
			return true
		}
	}
	return false
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

// Returns true if the workload defined by the GVK is supported by the VPA Autopilot, false otherwise
func IsTargetWorkloadSupported(workloadGVK schema.GroupVersionKind) bool {
	for _, supportedWorkload := range ListKindSupported {
		if strings.EqualFold(supportedWorkload.ApiVersion, workloadGVK.GroupVersion().String()) && strings.EqualFold(supportedWorkload.Kind, workloadGVK.Kind) {
			return true
		}
	}
	return false
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
