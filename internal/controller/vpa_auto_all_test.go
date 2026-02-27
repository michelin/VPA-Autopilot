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
	"encoding/json"
	"fmt"
	"time"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/testutils"
	"github.com/michelin/vpa-autopilot/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("VPA Auto Controllers", func() {
	timeout := 5 * time.Second
	Context("When reconciling a resource", func() {
		for _, workloadType := range listKindSupported {
			// Test that a VPA is created by the workload with the expected target and labels
			It(fmt.Sprintf("Creates the automatic VPA when a %s is created", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				time.Sleep(5 * time.Second)

				By("Checking the content of the VPA")
				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())
				vpa := vpaList[0]

				Expect(vpa.Labels).Should(HaveKeyWithValue(config.VpaLabelKey, config.VpaLabelValue))

				Expect(vpa.Spec.TargetRef.APIVersion).Should(BeIdenticalTo(workloadType.apiVersion))
				Expect(vpa.Spec.TargetRef.Kind).Should(BeIdenticalTo(workloadType.kind))
				Expect(vpa.Spec.TargetRef.Name).Should(BeIdenticalTo(workload.GetName()))

				Expect(vpa.Spec.ResourcePolicy.ContainerPolicies).Should(HaveLen(1))
				Expect(*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledResources).Should(HaveLen(1))
				Expect((*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledResources)[0]).Should(BeIdenticalTo(corev1.ResourceCPU))

				Expect(*vpa.Spec.UpdatePolicy.UpdateMode).Should(BeEquivalentTo(config.VpaBehaviourTyped))

				Expect(vpa.Annotations).Should(HaveKeyWithValue("vpa-autopilot.michelin.com/original-requests-sum", "1050"))
			})

			// Test that the automatic VPA is created with the correct controlled value
			It("Creates the automatic VPA with the correct controlled value", func() {
				By("Setting controlled value to RequestOnly")
				config.TargetLimits = false
				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking the content of the VPA")
				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())
				vpa := vpaList[0]
				Expect(*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues).Should(BeIdenticalTo(vpav1.ContainerControlledValuesRequestsOnly))

				By("Setting controlled value to RequestAndLimits")
				config.TargetLimits = true
				By(fmt.Sprintf("Creating a 2nd test %s", workloadType.kind))
				workload2, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload2)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload2)).To(Succeed())
				}()

				By("Checking the content of the VPA")
				var vpaList2 []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList2 = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload2.GetName(), workload2.GetNamespace())
					return len(vpaList2) == 1
				}, timeout).Should(BeTrue())
				vpa2 := vpaList2[0]
				Expect(*vpa2.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues).Should(BeIdenticalTo(vpav1.ContainerControlledValuesRequestsAndLimits))
			})
			// Test that the automatic VPA is recreated if deleted (as long as the target workload is still there)
			It(fmt.Sprintf("Recreates the automatic VPA if it is deleted while the %s is still here", workloadType.kind), func() {
				By("Setting up an automatic VPA")
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				By("Deleting the automatic VPA")
				Expect(k8sClient.Delete(ctx, vpaList[0])).To(Succeed())

				By("Checking that the VPA is present again")
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())
			})

			// Test that the automatic VPA is reconciled if it is updated (as long as the target workload is still there)
			It(fmt.Sprintf("Fixes the automatic VPA specs if it is modified while the %s is still here", workloadType.kind), func() {
				By("Setting up an automatic VPA")
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				oldVPATargetName := vpaList[0].Spec.TargetRef.DeepCopy().Name

				By("Modifying an element of the automatic VPA")
				modVPA := vpaList[0]
				modVPA.Spec.TargetRef.Name = "shouldNotBeHere"
				patch, err := json.Marshal(modVPA)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(k8sClient.Patch(ctx, modVPA, client.RawPatch(types.MergePatchType, patch))).To(Succeed())

				time.Sleep(5 * time.Second)

				By("Checking that the VPA reverted to the correct specs")
				currentVPA := &vpav1.VerticalPodAutoscaler{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: modVPA.Namespace, Name: modVPA.Name}, currentVPA)).To(Succeed())
				Expect(currentVPA.Spec.TargetRef.Name).Should(BeIdenticalTo(oldVPATargetName))
			})

			// Test that the excluded namespaces are correctly ignored by the operator with labels
			It("Ignores the namespaces present in excluded-namespaces", func() {
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-vpa-ignore-namespace",
						Labels: map[string]string{
							config.ExcludedNamespaceLabelKey: config.ExcludedNamespaceLabelValue,
						},
					},
				}
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				workload.SetNamespace(namespace.Name)

				err = k8sClient.Create(ctx, &namespace)
				if err != nil && !errors.IsAlreadyExists(err) { // It is OK if the namespace already exists, as it can be created by a previous test on a different kind
					Expect(err).To(Not(HaveOccurred()))
				}

				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
					// Do not delete the namespace as it will be stuck in deleteing state
				}()

				By("Checking that no VPA was created")
				Consistently(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList)
				}, timeout).Should(BeNumerically("==", 0))
			})

			// Test that the excluded namespaces in the list are correctly ignored by the operator
			It("Ignores the namespaces present in excluded-namespaces", func() {
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-vpa-ignore-namespace-list",
					},
				}
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				workload.SetNamespace(namespace.Name)

				err = k8sClient.Create(ctx, &namespace)
				if err != nil && !errors.IsAlreadyExists(err) { // It is OK if the namespace already exists, as it can be created by a previous test on a different kind
					Expect(err).To(Not(HaveOccurred()))
				}
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
					// Do not delete the namespace as it will be stuck in deleteing state
				}()
				By("Checking that no VPA was created")
				Consistently(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList)
				}, timeout).Should(BeNumerically("==", 0))
			})

			// Test that the automatic VPA is reconciled if its label is updated (as long as the workload is still there)
			It(fmt.Sprintf("Fixes the automatic VPA label if it is modified while the %s is still here", workloadType.kind), func() {
				By("Setting up an automatic VPA")
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				oldVPALabels := vpaList[0].DeepCopy().Labels
				By("Modifying an element of the automatic VPA")
				modVPA := vpaList[0]
				modVPA.ObjectMeta.Labels = map[string]string{
					"modifiedLabel": "foo",
				}
				Expect(k8sClient.Update(ctx, modVPA)).To(Succeed())

				By("Checking that the VPA reverted to the correct specs")
				Eventually(func() map[string]string {
					currentVPA := &vpav1.VerticalPodAutoscaler{}
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: modVPA.Namespace, Name: modVPA.Name}, currentVPA)).To(Succeed())
					return currentVPA.Labels
				}).Should(BeEquivalentTo(oldVPALabels))
			})

			// Test that the automatic VPA is not created if another VPA targets the same workload
			It(fmt.Sprintf("Ignores the %s if another VPA targets it", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a VPA for the future %s", workloadType.kind))
				clientVPA := testutils.GenerateTestClientVPA(ctx, "default", workloadType.apiVersion, workloadType.kind, "test-clientvpa-ignored")
				Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
				}()

				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind, clientVPA.Spec.TargetRef.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that no automatic VPA was created")
				Consistently(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					automaticVPANumber := 0
					for _, vpa := range vpaList {
						if value, present := vpa.GetLabels()[config.VpaLabelKey]; present {
							if value == config.VpaLabelValue {
								automaticVPANumber += 1
							}
						}
					}
					return automaticVPANumber
				}, timeout).Should(BeNumerically("==", 0))
			})

			// Test that the automatic VPA is not created if a HPA targets the same workload
			It(fmt.Sprintf("Ignores the %s if a HPA targets it", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a HPA for the future %s", workloadType.kind))
				hpa := testutils.GenerateTestClientHPA(ctx, "default", workloadType.apiVersion, workloadType.kind, "testclienthpa-ignored")
				Expect(k8sClient.Create(ctx, hpa)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, hpa)).To(Succeed())
				}()

				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind, hpa.Spec.ScaleTargetRef.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that no automatic VPA was created")
				Consistently(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					automaticVPANumber := 0
					for _, vpa := range vpaList {
						if value, present := vpa.GetLabels()[config.VpaLabelKey]; present {
							if value == config.VpaLabelValue {
								automaticVPANumber += 1
							}
						}
					}
					return automaticVPANumber
				}, timeout).Should(BeNumerically("==", 0))
			})
		}
	})
})
