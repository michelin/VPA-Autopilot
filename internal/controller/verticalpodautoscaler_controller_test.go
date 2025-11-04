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
	"time"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/testutils"
	"github.com/michelin/vpa-autopilot/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("VerticalPodAutoscaler Controller", func() {
	timeout := 5 * time.Second
	Context("When reconciling a resource", func() {

		// Test that the automatic VPA is deleted if another VPA that targets the same deployment is created
		It("Deletes the automatic VPA if another VPA targeting the deployment is created", func() {
			By("Setting up an automatic VPA")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking that the VPA was created")
			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())

			By("Creating another VPA for the deployment")
			clientVPA := testutils.GenerateTestClientVPA(ctx, deployment.Namespace, deployment.Name)
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
			}()

			By("Checking that the automatic VPA was deleted")
			Eventually(func() int {
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

		// Test that the automatic VPA is created if the other VPA that targets the same deployment is deleted
		It("Creates the automatic VPA if the other VPA targeting the deployment is deleted", func() {
			By("Creating a VPA for the future deployment")
			clientVPA := testutils.GenerateTestClientVPA(ctx, "default", "test-delete-clientvpa-creates-automaticvpa")
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())

			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment(clientVPA.Spec.TargetRef.Name)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking that the automatic VPA was not present")
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

			By("Deleting the test client VPA")
			Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())

			By("Checking the automatic VPA is created")
			Eventually(func() int {
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
			}, timeout).Should(BeNumerically("==", 1))
		})

		It("Deletes the automatic VPA if another VPA is updated to target the deployment", func() {
			By("Creating a test deployment")
			deployment := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}()

			By("Checking that the VPA was created")
			var vpaList []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace})
				return len(vpaList) == 1
			}, timeout).Should(BeTrue())

			By("Creating another VPA for the deployment")
			clientVPA := testutils.GenerateTestClientVPA(ctx, deployment.Namespace, deployment.Name)
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
			}()

			By("Checking that the automatic VPA was deleted")
			Eventually(func() int {
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

		It("Updates the automatic VPAs correctly when another VPA change targets", func() {
			By("Creating two tests deployments")
			deployment1 := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment1)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment1)).To(Succeed())
			}()

			deployment2 := testutils.GenerateTestDeployment()
			Expect(k8sClient.Create(ctx, deployment2)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, deployment2)).To(Succeed())
			}()

			By("Checking that the VPAs were created")
			var vpaList1 []*vpav1.VerticalPodAutoscaler
			var vpaList2 []*vpav1.VerticalPodAutoscaler
			Eventually(func() bool {
				vpaList1 = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment1.Name, Namespace: deployment1.Namespace})
				vpaList2 = utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment2.Name, Namespace: deployment2.Namespace})
				return len(vpaList1) == 1 && len(vpaList2) == 1
			}, timeout).Should(BeTrue())

			By("Creating another VPA for the deployment 1")
			clientVPA := testutils.GenerateTestClientVPA(ctx, deployment1.Namespace, deployment1.Name)
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
			}()

			By("Checking that the automatic VPA for deployment 1 was deleted")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment1.Name, Namespace: deployment1.Namespace})
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

			By("Changing the target of the manual VPA")
			clientVPA.Spec.TargetRef.Name = deployment2.Name
			Expect(k8sClient.Update(ctx, clientVPA)).To(Succeed())

			By("Checking that the deployment 1 got its automatic VPA back")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment1.Name, Namespace: deployment1.Namespace})
				automaticVPANumber := 0
				for _, vpa := range vpaList {
					if value, present := vpa.GetLabels()[config.AutoVpaGoVpaLabelKey]; present {
						if value == config.AutoVpaGoVpaLabelValue {
							automaticVPANumber += 1
						}
					}
				}
				return automaticVPANumber
			}, timeout).Should(BeNumerically("==", 1))

			By("Checking that the automatic VPA for deployment 2 was deleted")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: deployment2.Name, Namespace: deployment2.Namespace})
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

		It("Updates the automatic VPAs correctly when another VPA changes from and to target a non existing VPA", func() {
			By("Creating another VPA for a non existing deployment")
			clientVPA := testutils.GenerateTestClientVPA(ctx, "default", "i-do-not-exist")
			Expect(k8sClient.Create(ctx, clientVPA)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, clientVPA)).To(Succeed())
			}()

			By("Checking that the automatic VPA for non existing deployment does not exists")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: "i-do-not-exist", Namespace: "default"})
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

			By("Changing the target of the manual VPA")
			clientVPA.Spec.TargetRef.Name = "i-do-not-exist-2"
			Expect(k8sClient.Update(ctx, clientVPA)).To(Succeed())

			By("Checking that the first non existing deployment still has no automatic VPA")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: "i-do-not-exist", Namespace: "default"})
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

			By("Checking that the automatic VPA for the non existing deployment does not exist")
			Eventually(func() int {
				vpaList := utils.FindMatchingVPA(ctx, k8sClient, types.NamespacedName{Name: "i-do-not-exist-2", Namespace: "default"})
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
