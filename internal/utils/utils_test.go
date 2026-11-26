package utils

import (
	"strings"
	"testing"
)

func TestAutomaticVPAName(t *testing.T) {
	if got, want := AutomaticVPAName("Deployment", "my-app"), "vpa-autopilot-deployment-my-app"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if AutomaticVPAName("Deployment", "my-app") == AutomaticVPAName("StatefulSet", "my-app") {
		t.Error("workloads of different kinds with the same name must get different VPA names")
	}

	// Long names that only differ by kind must stay distinct and valid
	longName := strings.Repeat("a", 250) + "-" + strings.Repeat("b", 2)
	deploymentName := AutomaticVPAName("Deployment", longName)
	statefulSetName := AutomaticVPAName("StatefulSet", longName)
	if deploymentName == statefulSetName {
		t.Error("truncated VPA names of different kinds must differ")
	}
	for _, name := range []string{deploymentName, statefulSetName} {
		if len(name) > maxVPANameLength {
			t.Errorf("name length %d exceeds %d", len(name), maxVPANameLength)
		}
	}
}
