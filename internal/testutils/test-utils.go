package testutils

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/michelin/vpa-autopilot/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

// Wrapper that generate an object of the requested kind
// Useful to factorize tests that are ommon on several object kinds
func GenerateTestWorkload(kind string, forceName ...string) (client.Object, error) {
	switch strings.ToLower(kind) {
	case "deployment":
		return GenerateTestDeployment(forceName...), nil
	case "statefulset":
		return GenerateTestStatefulset(forceName...), nil
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", kind)
	}
}

func GenerateTestDeployment(forceName ...string) *appsv1.Deployment {
	var deploymentName string
	if len(forceName) != 0 {
		deploymentName = forceName[0]
	} else {
		deploymentNameBuilder := strings.Builder{}
		deploymentNameBuilder.Grow(10)
		for i := 0; i < 10; i++ {
			deploymentNameBuilder.WriteByte(charset[rand.Intn(len(charset))])
		}
		deploymentName = deploymentNameBuilder.String()
	}
	deploymentMetadata := metav1.ObjectMeta{
		Name:      deploymentName,
		Namespace: "default",
		Labels: map[string]string{
			"test": "deployment",
		},
	}
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"test": "pod",
		},
	}
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: deploymentMetadata,
		Spec: appsv1.DeploymentSpec{
			Selector: &podSelector,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podSelector.MatchLabels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: config.AutoVpaGoTestDeploymentImage,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1050m"),
								},
							},
						},
					},
				},
			},
		},
	}
	return deployment
}

func GenerateTestStatefulset(forceName ...string) *appsv1.StatefulSet {
	var statefulsetName string
	if len(forceName) != 0 {
		statefulsetName = forceName[0]
	} else {
		statefulsetNameBuilder := strings.Builder{}
		statefulsetNameBuilder.Grow(10)
		for i := 0; i < 10; i++ {
			statefulsetNameBuilder.WriteByte(charset[rand.Intn(len(charset))])
		}
		statefulsetName = statefulsetNameBuilder.String()
	}
	statefulsetMetadata := metav1.ObjectMeta{
		Name:      statefulsetName,
		Namespace: "default",
		Labels: map[string]string{
			"test": "statefulset",
		},
	}
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"test": "pod",
		},
	}
	statefulset := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: statefulsetMetadata,
		Spec: appsv1.StatefulSetSpec{
			Selector: &podSelector,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podSelector.MatchLabels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test-container",
							Image: config.AutoVpaGoTestDeploymentImage,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("1050m"),
								},
							},
						},
					},
				},
			},
		},
	}
	return statefulset
}

func GenerateTestClientVPA(ctx context.Context, namespace string, targetApiVersion string, targetKind string, targetName string) *vpav1.VerticalPodAutoscaler {
	vpaNameBuilder := strings.Builder{}
	vpaNameBuilder.Grow(10)
	for i := 0; i < 10; i++ {
		vpaNameBuilder.WriteByte(charset[rand.Intn(len(charset))])
	}
	vpa := &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vpaNameBuilder.String(),
			Namespace: namespace,
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: targetApiVersion,
				Kind:       targetKind,
				Name:       targetName,
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &config.VpaBehaviourTyped,
			},
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						ControlledResources: &[]corev1.ResourceName{
							corev1.ResourceCPU,
						},
					},
				},
			},
		},
	}
	return vpa
}

func GenerateTestClientHPA(ctx context.Context, namespace string, targetApiVersion string, targetKind string, targetName string) *autoscaling.HorizontalPodAutoscaler {
	var minReplicas int32 = 1
	var maxReplicas int32 = 2
	var target int32 = 100
	targetRef := &autoscaling.CrossVersionObjectReference{
		APIVersion: targetApiVersion,
		Kind:       targetKind,
		Name:       targetName,
	}

	hpaNameBuilder := strings.Builder{}
	hpaNameBuilder.Grow(10)
	for i := 0; i < 10; i++ {
		hpaNameBuilder.WriteByte(charset[rand.Intn(len(charset))])
	}

	hpa := &autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaNameBuilder.String(),
			Namespace: namespace,
		},
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			MinReplicas:                    &minReplicas,
			MaxReplicas:                    maxReplicas,
			TargetCPUUtilizationPercentage: &target,
			ScaleTargetRef:                 *targetRef,
		},
	}
	return hpa
}
