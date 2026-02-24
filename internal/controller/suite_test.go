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
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	config "github.com/michelin/vpa-autopilot/internal/config"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client

var mgr ctrl.Manager

var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

type supportedWorkloadTypes struct {
	apiVersion string
	kind       string
}

var listKindSupported = []supportedWorkloadTypes{
	{apiVersion: "apps/v1", kind: "Deployment"},
	{apiVersion: "apps/v1", kind: "StatefulSet"},
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

func setupAutoVPAController(ctx context.Context) {
	autoVPADeploymentReconciler := &AutoVPADeploymentReconciler{
		Client:       mgr.GetClient(),
		Scheme:       k8sClient.Scheme(),
		RequeueAfter: 5 * time.Second,
	}
	err := autoVPADeploymentReconciler.SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	autoVPAStatefulSetReconciler := &AutoVPAStatefulSetReconciler{
		Client:       mgr.GetClient(),
		Scheme:       k8sClient.Scheme(),
		RequeueAfter: 5 * time.Second,
	}
	err = autoVPAStatefulSetReconciler.SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	vpaReconciler := &VerticalPodAutoscalerReconciler{
		Client: mgr.GetClient(),
		Scheme: k8sClient.Scheme(),
	}
	err = vpaReconciler.SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	hpaReconciler := &HorizontalPodAutoscalerReconciler{
		Client: mgr.GetClient(),
		Scheme: k8sClient.Scheme(),
	}
	err = hpaReconciler.SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred(), "failed to run manager")
	}()
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.31.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	config.VpaLabelValue = "vpa-autopilot-managed"
	config.VpaLabelKey = "true"
	config.VpaBehaviourTyped = "Off"
	config.ExcludedNamespaceLabelKey = "excluded-namespaces-key"
	config.ExcludedNamespaceLabelValue = "true"
	config.ExcludedNamespaces.Set("test-vpa-ignore-namespace-list")

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = appsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = vpav1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = autoscalingv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: k8sClient.Scheme(),
	})
	Expect(err).NotTo(HaveOccurred())
	setupAutoVPAController(ctx)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
