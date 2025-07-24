package helper

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	pwcorev1alpha1 "github.com/openmcp-project/project-workspace-operator/api/core/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ProjectName   = "project"
	WorkspaceName = "workspace"
	MCPName       = "test-mcp"

	ChargingTargetLabelKey = "openmcp.cloud.sap/charging-target"
	ChargingTarget         = "12345678"
)

var (
	projectNamespaceName   string
	workspaceNamespaceName string
)

var _ = Describe("Charging Target Resolver", Ordered, func() {
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
					ChargingTargetLabelKey: ChargingTarget,
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

	It("Should resolve the charging target", func() {
		ctx := context.Background()
		resolvedChargingTarget, err := ResolveChargingTarget(ctx, k8sClient, ProjectName, WorkspaceName, MCPName)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(resolvedChargingTarget).Should(Equal(ChargingTarget))
	})

	It("Should resolve the workspace charging target, if set", func() {
		ctx := context.Background()

		workspace := pwcorev1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      WorkspaceName,
				Namespace: projectNamespaceName,
			},
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&workspace), &workspace)).Should(Succeed())

		workspace.SetLabels(map[string]string{
			ChargingTargetLabelKey: "9876543",
		})
		Expect(k8sClient.Update(ctx, &workspace)).Should(Succeed())

		resolvedChargingTarget, err := ResolveChargingTarget(ctx, k8sClient, ProjectName, WorkspaceName, MCPName)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(resolvedChargingTarget).Should(Equal("9876543"))
	})

	It("Should resolve the mcp charging target, if set", func() {
		ctx := context.Background()

		mcp := corev1alpha1.ManagedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MCPName,
				Namespace: workspaceNamespaceName,
			},
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcp), &mcp)).Should(Succeed())

		mcp.SetLabels(map[string]string{
			ChargingTargetLabelKey: "14689283",
		})
		Expect(k8sClient.Update(ctx, &mcp)).Should(Succeed())

		resolvedChargingTarget, err := ResolveChargingTarget(ctx, k8sClient, ProjectName, WorkspaceName, MCPName)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(resolvedChargingTarget).Should(Equal("14689283"))
	})
})
