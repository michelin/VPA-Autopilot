package config

import (
	"fmt"
	"strings"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

type excludedNamespaces []string

// For excludedNamespaces: implement String of the flag.Value interface
func (i *excludedNamespaces) String() string {
	return fmt.Sprintf("%v", *i)
}

// For excludedNamespaces: implement Set is implementation of the flag.Value interface
func (i *excludedNamespaces) Set(value string) error {
	*i = strings.Split(strings.Trim(value, ","), ",")
	return nil
}

const (
	// AutoVpaGoTestDeploymentImage is the image used in the deployments used in the tests
	AutoVpaGoTestDeploymentImage string = "registry.k8s.io/pause:latest" // Currently only a placeholder image, pods are not really created

	// ControllerCreateError error message when the controller cannot be created
	ControllerCreateError string = "unable to create controller"

	// DeploymentGetError error message when a deployment cannot be retrieved
	DeploymentGetError string = "Unable to get Deployment"

	// VPAGenerationError error message when a VPA cannot be generated
	VPAGenerationError string = "Unable to generate VPA specs"
)

var (
	// AutoVpaGoVpaNamePrefix is the prefix used to generate the name of the automatic VPA
	AutoVpaGoVpaNamePrefix string = "vpa-autopilot-"

	// AutoVpaGoVpaLabelKey is the label key used to mark a VPA managed by the controller
	AutoVpaGoVpaLabelKey string
	// AutoVpaGoVpaLabelValue is the label value used to mark a VPA managed by the controller
	AutoVpaGoVpaLabelValue string

	// AutopaGoVpaBehaviour is the behavior of the VPAs managed by the controller.
	// It should be one of the value available in vpa.spec.updatePolicy.updateMode
	AutopaGoVpaBehaviourString string

	// ExcludedNamespaces contains the list of namespaces (comma-separated)
	// that should be ignored by the cluster controller
	// it is set from the environment variable EXCLUDED_NAMESPACES if defined
	ExcludedNamespaces excludedNamespaces
	// ExcludedNamespaceLabelKey and ExcludedNamespaceLabelValue set the selector for the namespaces
	// that should be ignored by the controller
	ExcludedNamespaceLabelKey   string
	ExcludedNamespaceLabelValue string

	// AutopaGoVpaBehaviour corresponds to the behaviour of the generated VPAs (check the VPA documentation for modes meaning)
	AutopaGoVpaBehaviour vpav1.UpdateMode

	// TargetLimits is a flag to select wehter the generated VPA change the limits of the deployments or not
	TargetLimits bool
)
