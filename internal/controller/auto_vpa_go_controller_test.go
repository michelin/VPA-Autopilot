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
	"time"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/testutils"
	"github.com/michelin/vpa-autopilot/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("Auto VPA Controller", func() {
	timeout := 5 * time.Second
	Context("When reconciling a resource", func() {
		// Test that a VPA is created by the deployment with the expected target and labels
		It("Creates the automatic VPA when a deployment is created", func() {
			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			time.Sleep(5 * time.Second)

			By("Checking the content of the VPA")
			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())
			vpa := vpaList[0]

			Expect(vpa.Labels).Should(HaveKeyWithValue(config.AutoVpaGoVpaLabelKey, config.AutoVpaGoVpaLabelValue))

			Expect(vpa.Spec.TargetRef.APIVersion).Should(BeIdenticalTo("apps/v1"))
			Expect(vpa.Spec.TargetRef.Kind).Should(BeIdenticalTo("Deployment"))
			Expect(vpa.Spec.TargetRef.Name).Should(BeIdenticalTo(deployment.Name))

			Expect(vpa.Spec.ResourcePolicy.ContainerPolicies).Should(HaveLen(1))
			Expect(*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledResources).Should(HaveLen(1))
			Expect((*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledResources)[0]).Should(BeIdenticalTo(corev1.ResourceCPU))

			Expect(*vpa.Spec.UpdatePolicy.UpdateMode).Should(BeEquivalentTo(config.AutopaGoVpaBehaviour))

			Expect(*&vpa.Annotations).Should(HaveKeyWithValue("vpa-autopilot.michelin.com/original-requests-sum", "1050"))
		})

		// Test that the automatic VPA is created with the correct controlled value
		It("Creates the automatic VPA with the correct controlled value", func() {
			By("Setting controlled value to RequestOnly")
			config.TargetLimits = false
			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking the content of the VPA")
			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())
			vpa := vpaList[0]
			Expect(*vpa.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues).Should(BeIdenticalTo(vpav1.ContainerControlledValuesRequestsOnly))

			By("Setting controlled value to RequestAndLimits")
			config.TargetLimits = true
			By("Creating a 2nd test deployment")
			deployment2 := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment2)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment2)).To(Succeed())
			}()

			By("Checking the content of the VPA")
			var vpaList2 []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList2 = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment2.Name, Namespace: deployment2.Namespace})
				return len(vpaList2) == 1
			}, timeout).Should(BeTrue())
			vpa2 := vpaList2[0]
			Expect(*vpa2.Spec.ResourcePolicy.ContainerPolicies[0].ControlledValues).Should(BeIdenticalTo(vpav1.ContainerControlledValuesRequestsAndLimits))
		})
		// Test that the automatic VPA is recreated if deleted (as long as the deployment is still there)
		It("Recreates the automatic VPA if it is deleted while the deployment is still here", func() {
			By("Setting up an automatic VPA")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())

			By("Deleting the automatic VPA")
			Expect(k8sClient.Delete(ctx, vpaList[0])).To(Succeed())

			By("Checking that the VPA is present again")
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())
		})

		// Test that the automatic VPA is reconciled if it is updated (as long as the deployment is still there)
		It("Fixes the automatic VPA specs if it is modified while the deployment is still here", func() {
			By("Setting up an automatic VPA")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
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
			deployment := testutils.GenerateTestDeployment()
			deployment.Namespace = namespace.Name
			Expect(k8sClient.Create(ctx, &namespace)).To(Succeed())
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
				Expect(k8sClient.Delete(ctx, &namespace)).To(Succeed())
			}()

			By("Checking that no VPA was created")
			Consistently(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
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
			deployment := testutils.GenerateTestDeployment()
			deployment.Namespace = namespace.Name
			Expect(k8sClient.Create(ctx, &namespace)).To(Succeed())
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
				Expect(k8sClient.Delete(ctx, &namespace)).To(Succeed())
			}()

			By("Checking that no VPA was created")
			Consistently(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList)
			}, timeout).Should(BeNumerically("==", 0))
		})

		// Test that the automatic VPA is reconciled if its label is updated (as long as the deployment is still there)
		It("Fixes the automatic VPA label if it is modified while the deployment is still here", func() {
			By("Setting up an automatic VPA")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
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

		// Test that the automatic VPA is not created if another VPA targets the same deployment
		It("Ignores the deployment if another VPA targets it", func() {
			By("Creating a VPA for the future deployment")
			clientVPA := testutils.GenerateTestClientVPA(ctx, "default", "test-clientvpa-ignored")
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
			}()

			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment(clientVPA.Spec.TargetRef.Name)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking that no automatic VPA was created")
			Consistently(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				automaticVPANumber := 0
				for _, vpa := range vpaList {
					if value, present := vpa.GetLabels()[config.AutoVpaGoVpaLabelKey]; present {
						if value == config.AutoVpaGoVpaLabelValue {
							automaticVPANumber += 1
						}
					}
				}
				return automaticVPANumber
			}, timeout).Should(BeNumerically("==", 0))
		})

		// Test that the automatic VPA is not created if a HPA targets the same deployment
		It("Ignores the deployment if a HPA targets it", func() {
			By("Creating a HPA for the future deployment")
			hpa := testutils.GenerateTestClientHPA(ctx, "default", "testclienthpa-ignored")
			Expect(k8sClient.Create(ctx, hpa)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, hpa)).To(Succeed())
			}()

			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment(hpa.Spec.ScaleTargetRef.Name)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking that no automatic VPA was created")
			Consistently(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				automaticVPANumber := 0
				for _, vpa := range vpaList {
					if value, present := vpa.GetLabels()[config.AutoVpaGoVpaLabelKey]; present {
						if value == config.AutoVpaGoVpaLabelValue {
							automaticVPANumber += 1
						}
					}
				}
				return automaticVPANumber
			}, timeout).Should(BeNumerically("==", 0))
		})
	})
})
