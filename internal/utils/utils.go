package utils

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"

	"github.com/michelin/vpa-autopilot/internal/config"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// generation a VPA for the Deployment 'deployment'
//   - return the VPA to create and nil if the operation was successful
//     or nil and an error if the operation failed
func GenerateAutomaticVPA(deployment *appsv1.Deployment) (*vpav1.VerticalPodAutoscaler, error) {
	// Generating the automatic VPA name
	// Hashing the deployment name to provide unique identifier to the automatic VPA (10 chars)
	deploymentHash := fnv.New32a()
	deploymentHash.Write([]byte(deployment.Name))
	suffix := fmt.Sprint(deploymentHash.Sum32())
	completeName := config.AutoVpaGoVpaNamePrefix

	if len(deployment.Name) > 253-len(config.AutoVpaGoVpaNamePrefix)-len(suffix) {
		// If the deployment name is too long to be concatenated directly, truncate it and add sha to avoid name collision
		completeName += deployment.Name[:253-len(config.AutoVpaGoVpaNamePrefix)-len(suffix)] + suffix
	} else {
		// If the deployment name is reasonable, concatenate it directly
		completeName += deployment.Name
	}
	controllerFlag := true
	// compute the annotation list that will reference the sum of the user specified CPU requests of the containers
	var requestCpuSum int64
	requestCpuSum = 0
	for _, container := range deployment.Spec.Template.Spec.Containers {
		requestCpuSum += container.Resources.Requests.Cpu().MilliValue()
	}

	controlledCpuValues := vpav1.ContainerControlledValuesRequestsOnly
	if config.TargetLimits {
		controlledCpuValues = vpav1.ContainerControlledValuesRequestsAndLimits
	}

	vpa := &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      completeName,
			Namespace: deployment.Namespace,
			Annotations: map[string]string{
				"vpa-autopilot.michelin.com/original-requests-sum": strconv.FormatInt(requestCpuSum, 10),
			},
			Labels: map[string]string{
				config.AutoVpaGoVpaLabelKey: config.AutoVpaGoVpaLabelValue,
			},
			// Set owner reference to the targeted deployment to facilitate deletion
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: deployment.APIVersion,
					Kind:       deployment.Kind,
					Name:       deployment.Name,
					UID:        deployment.UID,
					Controller: &controllerFlag,
				},
			},
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployment.Name,
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &config.AutopaGoVpaBehaviour,
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
	// try to get the automatic VPA associated to the deployment to determine if the automatic VPA needs to be created or updated
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

// Find all VPAs matching the deployment in the parameter
//   - returns a list with all the VPAs that were matched
func FindMatchingVPA(ctx context.Context, k8sclient client.Client, targetDeploymentNamespacedName types.NamespacedName) []*vpav1.VerticalPodAutoscaler {
	vpaList := &vpav1.VerticalPodAutoscalerList{}
	_ = k8sclient.List(ctx, vpaList, &client.ListOptions{Namespace: targetDeploymentNamespacedName.Namespace})
	matchingList := make([]*vpav1.VerticalPodAutoscaler, 0)
	for _, vpa := range vpaList.Items {
		if vpa.Spec.TargetRef.Kind == "Deployment" && vpa.Spec.TargetRef.Name == targetDeploymentNamespacedName.Name {
			matchingList = append(matchingList, &vpa)
		}
	}
	return matchingList
}
