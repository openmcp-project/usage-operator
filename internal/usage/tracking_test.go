package usage

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
)

const (
	projectName   = "project"
	workspaceName = "workspace"
	mcpName       = "mcp-test"

	mcpUsageName = "test"
)

var usageTracker *UsageTracker

var _ = Describe("Tracking Module", Ordered, func() {
	BeforeAll(func() {
		ctx := context.Background()
		creationTime := metav1.NewTime(metav1.Now().Add(-time.Hour * 4))
		mcpUsage := v1.MCPUsage{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpUsageName,
			},
			Spec: v1.MCPUsageSpec{
				ChargingTarget:    "missing",
				Project:           projectName,
				Workspace:         workspaceName,
				MCP:               mcpName,
				MCPCreatedAt:      creationTime,
				LastUsageCaptured: creationTime,
			},
		}
		Expect(k8sClient.Create(ctx, &mcpUsage)).Should(Succeed())
	})

	It("Check scheduled Event", func() {
		ctx := context.Background()

		usageTracker, err := NewUsageTracker(k8sClient)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(usageTracker.ScheduledEvent(ctx)).Should(Succeed())

		mcpUsage := v1.MCPUsage{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpUsageName,
			},
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcpUsage), &mcpUsage)).Should(Succeed())

		Expect(mcpUsage.Spec.Usage).ShouldNot(HaveLen(0))
	})

	It("garbage collect old usage data", func() {
		ctx := context.Background()

		mcpUsage := v1.MCPUsage{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpUsageName,
			},
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcpUsage), &mcpUsage)).Should(Succeed())

		now := metav1.Now()
		mcpUsage.Spec.Usage = []v1.DailyUsage{
			{
				Date: metav1.NewTime(now.Add(-time.Hour * 4)),
				Usage: metav1.Duration{
					Duration: time.Hour * 4,
				},
			},
			{
				Date: metav1.NewTime(now.Add(-time.Hour * 24 * 40)),
				Usage: metav1.Duration{
					Duration: time.Hour * 4,
				},
			},
		}
		Expect(k8sClient.Update(ctx, &mcpUsage)).Should(Succeed())

		usageTracker, err := NewUsageTracker(k8sClient)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(usageTracker.GarbageCollection(ctx)).Should(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&mcpUsage), &mcpUsage)).Should(Succeed())
		Expect(mcpUsage.Spec.Usage).Should(HaveLen(1))
	})

	It("should create an mcp usage resource", func() {
		ctx := context.Background()

		usageTracker, err := NewUsageTracker(k8sClient)
		Expect(err).ShouldNot(HaveOccurred())

		objectKey, err := GetObjectKey(projectName, workspaceName, mcpName)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(usageTracker.CreateOrUpdateEvent(ctx, projectName, workspaceName, mcpName)).Should(Succeed())

		var mcpUsage v1.MCPUsage
		Expect(k8sClient.Get(ctx, objectKey, &mcpUsage)).Should(Succeed())

		Expect(mcpUsage.Spec.Project).Should(Equal(projectName))
		Expect(mcpUsage.Spec.Workspace).Should(Equal(workspaceName))
		Expect(mcpUsage.Spec.MCP).Should(Equal(mcpName))
	})

	It("should delete mcp usage resource", func() {
		ctx := context.Background()

		usageTracker, err := NewUsageTracker(k8sClient)
		Expect(err).ShouldNot(HaveOccurred())

		objectKey, err := GetObjectKey(projectName, workspaceName, mcpName)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(usageTracker.CreateOrUpdateEvent(ctx, projectName, workspaceName, mcpName)).Should(Succeed())
		Expect(usageTracker.DeletionEvent(ctx, projectName, workspaceName, mcpName)).Should(Succeed())

		var mcpUsage v1.MCPUsage
		Expect(k8sClient.Get(ctx, objectKey, &mcpUsage)).Should(Succeed())

		Expect(mcpUsage.Spec.MCPDeletedAt.IsZero()).Should(BeFalse())

		// It should also handle events for already deleted mcps
		Expect(usageTracker.CreateOrUpdateEvent(ctx, projectName, workspaceName, mcpName)).Should(Succeed())
	})
})
