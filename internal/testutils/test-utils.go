package testutils

import (
	"context"
	"math/rand"
	"strings"

	"github.com/michelin/vpa-autopilot/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

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
	deploymentKey := types.NamespacedName{
		Name:      deploymentName,
		Namespace: "default",
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentKey.Name,
			Namespace: deploymentKey.Namespace,
			Labels: map[string]string{
				"test": "deployment",
			},
		},
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

func GenerateTestClientVPA(ctx context.Context, namespace string, targetDeploymentName string) *vpav1.VerticalPodAutoscaler {
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
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetDeploymentName,
			},
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &config.AutopaGoVpaBehaviour,
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

func GenerateTestClientHPA(ctx context.Context, namespace string, targetDeploymentName string) *autoscaling.HorizontalPodAutoscaler {
	var minReplicas int32 = 1
	var maxReplicas int32 = 2
	var target int32 = 100
	targetRef := &autoscaling.CrossVersionObjectReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       targetDeploymentName,
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
