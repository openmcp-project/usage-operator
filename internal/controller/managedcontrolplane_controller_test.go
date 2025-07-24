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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	pwcorev1alpha1 "github.com/openmcp-project/project-workspace-operator/api/core/v1alpha1"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
)

const (
	ProjectName   = "project"
	WorkspaceName = "workspace"
	MCPName       = "test-mcp"

	ChargingTarget = "12345678"

	timeout  = time.Second * 10
	duration = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	projectNamespaceName   string
	workspaceNamespaceName string

	mcpUsageName string
)

var _ = Describe("ManagedControlPlane Controller", Ordered, func() {
	Context("When reconciling a resource", func() {
		BeforeAll(func() {
			ctx := context.Background()
			projectNamespaceName = "project-" + ProjectName
			workspaceNamespaceName = projectNamespaceName + "--ws-" + WorkspaceName
			namespaces := []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: projectNamespaceName,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: workspaceNamespaceName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &namespaces[0])).To(Succeed())
			Expect(k8sClient.Create(ctx, &namespaces[1])).To(Succeed())

			project := pwcorev1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name: ProjectName,
					Labels: map[string]string{
						"openmcp.cloud.sap/charging-target": ChargingTarget,
					},
				},
			}
			Expect(k8sClient.Create(ctx, &project)).To(Succeed())
			workspace := pwcorev1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      WorkspaceName,
					Namespace: projectNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &workspace)).To(Succeed())
			mcp := corev1alpha1.ManagedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MCPName,
					Namespace: workspaceNamespaceName,
				},
			}
			Expect(k8sClient.Create(ctx, &mcp)).To(Succeed())
		})

		It("should create a mcp usage resource based on a ManagedControlPlane resource", func() {
			ctx := context.Background()

			var mcpUsages v1.MCPUsageList
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, &mcpUsages)).To(Succeed())

				g.Expect(mcpUsages.Items).Should(HaveLen(1))

				mcpUsageName = mcpUsages.Items[0].Name

				g.Expect(mcpUsages.Items[0].Spec.ChargingTarget).Should(Equal(ChargingTarget))
			}, timeout, interval).Should(Succeed())
		})

		It("should have set the right charging target", func() {
			ctx := context.Background()

			mcpUsage := v1.MCPUsage{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcpUsageName,
				},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcpUsage), &mcpUsage)).Should(Succeed())

			Expect(mcpUsage.Spec.ChargingTarget).Should(Equal(ChargingTarget))
		})

		It("should mark a mcp usage resource as deleted when ManagedControlPlane is deleted", func() {
			ctx := context.Background()

			mcpUsage := v1.MCPUsage{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcpUsageName,
				},
			}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcpUsage), &mcpUsage)).Should(Succeed())

			var mcp corev1alpha1.ManagedControlPlane
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      strings.ToLower(MCPName),
				Namespace: workspaceNamespaceName,
			}, &mcp)).Should(Succeed())
			mcp.Status.Status = corev1alpha1.MCPStatusDeleting
			Expect(k8sClient.Status().Update(ctx, &mcp)).Should(Succeed())

			var mcpUsages v1.MCPUsageList
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, &mcpUsages)).To(Succeed())

				g.Expect(mcpUsages.Items).Should(HaveLen(1))

				g.Expect(mcpUsages.Items[0].Spec.MCPDeletedAt.IsZero()).Should(BeFalse())
			}, timeout, interval).Should(Succeed())
		})
	})
})
