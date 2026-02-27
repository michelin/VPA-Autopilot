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
	"fmt"
	"strings"
	"time"

	config "github.com/michelin/vpa-autopilot/internal/config"
	"github.com/michelin/vpa-autopilot/internal/testutils"
	"github.com/michelin/vpa-autopilot/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

var _ = Describe("HorizontalPodAutoscaler Controller", func() {
	timeout := 5 * time.Second
	Context("When reconciling a resource", func() {
		for _, workloadType := range listKindSupported {
			// Test that the automatic VPA is deleted if a HPA that targets the same workload is created
			It(fmt.Sprintf("Deletes the automatic VPA if another HPA targeting the %s is created", workloadType.kind), func() {
				By("Setting up an automatic VPA")
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that the VPA was created")
				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				By(fmt.Sprintf("Creating a HPA for the %s", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, workload.GetNamespace(), workloadType.apiVersion, workloadType.kind, workload.GetName())
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())
				}()

				By("Checking that the automatic VPA was deleted")
				Eventually(func() int {
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

			It(fmt.Sprintf("Deletes the automatic VPA if another HPA targeting the %s in lowercase is created", workloadType.kind), func() {
				By("Setting up an automatic VPA")
				workload, err := testutils.GenerateTestWorkload(strings.ToLower(workloadType.kind))
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that the VPA was created")
				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, strings.ToLower(workloadType.kind), workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				By(fmt.Sprintf("Creating a HPA for the %s", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, workload.GetNamespace(), workloadType.apiVersion, workloadType.kind, workload.GetName())
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())
				}()

				By("Checking that the automatic VPA was deleted")
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, strings.ToLower(workloadType.kind), workload.GetName(), workload.GetNamespace())
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

			// Test that the automatic VPA is created if the client HPA that targets the same workload is deleted
			It(fmt.Sprintf("Creates the automatic VPA if the client HPA targeting the %s is deleted", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a HPA for the future %s", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, "default", workloadType.apiVersion, workloadType.kind, "test-delete-clienthpa-creates-automaticvpa")
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())

				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind, clientHPA.Spec.ScaleTargetRef.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that the automatic VPA was not present")
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

				By("Deleting the test client HPA")
				Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())

				By("Checking the automatic VPA is created")
				Eventually(func() int {
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
				}, timeout).Should(BeNumerically("==", 1))
			})

			It(fmt.Sprintf("Deletes the automatic VPA if a client HPA is updated to target the %s", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a test %s", workloadType.kind))
				workload, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload)).To(Succeed())
				}()

				By("Checking that the automatic VPA was created")
				var vpaList []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload.GetName(), workload.GetNamespace())
					return len(vpaList) == 1
				}, timeout).Should(BeTrue())

				By(fmt.Sprintf("Creating a HPA for the %s", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, workload.GetNamespace(), workloadType.apiVersion, workloadType.kind, workload.GetName())
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())
				}()

				By("Checking that the automatic VPA was deleted")
				Eventually(func() int {
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

			It(fmt.Sprintf("Updates the automatic VPAs correctly when a HPA changes %s targets", workloadType.kind), func() {
				By(fmt.Sprintf("Creating two test %s", workloadType.kind))
				workload1, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload1)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload1)).To(Succeed())
				}()

				workload2, err := testutils.GenerateTestWorkload(workloadType.kind)
				Expect(err).ToNot(HaveOccurred())
				Expect(k8sClient.Create(ctx, workload2)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, workload2)).To(Succeed())
				}()

				By("Checking that the VPAs were created")
				var vpaList1 []*vpav1.VerticalPodAutoscaler
				var vpaList2 []*vpav1.VerticalPodAutoscaler
				Eventually(func() bool {
					vpaList1 = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload1.GetName(), workload1.GetNamespace())
					vpaList2 = utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload2.GetName(), workload2.GetNamespace())
					return len(vpaList1) == 1 && len(vpaList2) == 1
				}, timeout).Should(BeTrue())

				By(fmt.Sprintf("Creating another HPA for the %s 1", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, workload1.GetNamespace(), workloadType.apiVersion, workloadType.kind, workload1.GetName())
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())
				}()

				By(fmt.Sprintf("Checking that the automatic VPA for %s 1 was deleted", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload1.GetName(), workload1.GetNamespace())
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

				By("Changing the target of the client HPA")
				clientHPA.Spec.ScaleTargetRef.Name = workload2.GetName()
				Expect(k8sClient.Update(ctx, clientHPA)).To(Succeed())
				By(fmt.Sprintf("Checking that the %s 1 got its automatic VPA back", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload1.GetName(), workload1.GetNamespace())
					automaticVPANumber := 0
					for _, vpa := range vpaList {
						if value, present := vpa.GetLabels()[config.VpaLabelKey]; present {
							if value == config.VpaLabelValue {
								automaticVPANumber += 1
							}
						}
					}
					return automaticVPANumber
				}, timeout).Should(BeNumerically("==", 1))

				By(fmt.Sprintf("Checking that the automatic VPA for %s 2 was deleted", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, workload2.GetName(), workload2.GetNamespace())
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

			It(fmt.Sprintf("Updates the automatic VPAs correctly when a HPA changes from and to target a non existing %s", workloadType.kind), func() {
				By(fmt.Sprintf("Creating a HPA for a non existing %s", workloadType.kind))
				clientHPA := testutils.GenerateTestClientHPA(ctx, "default", workloadType.apiVersion, workloadType.kind, "i-do-not-exist")
				Expect(k8sClient.Create(ctx, clientHPA)).To(Succeed())
				defer func() {
					Expect(k8sClient.Delete(ctx, clientHPA)).To(Succeed())
				}()

				By(fmt.Sprintf("Checking that the automatic VPA for non existing %s does not exists", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, "i-do-not-exist", "default")
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

				By("Changing the target of the manual HPA")
				clientHPA.Spec.ScaleTargetRef.Name = "i-do-not-exist-2"
				Expect(k8sClient.Update(ctx, clientHPA)).To(Succeed())

				By(fmt.Sprintf("Checking that the first non existing %s still has no automatic VPA", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, "i-do-not-exist", "default")
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

				By(fmt.Sprintf("Checking that the automatic VPA for the non existing %s does not exist", workloadType.kind))
				Eventually(func() int {
					vpaList := utils.FindMatchingVPA(ctx, k8sClient, workloadType.kind, "i-do-not-exist-2", "default")
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
