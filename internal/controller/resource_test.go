package controller_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/controller"
)

// newResourceController builds a TrackedResourceController from the given environment.
func newResourceController(env *testutils.ComplexEnvironment) *controller.TrackedResourceController {
	return controller.NewTrackedResourceController(clusters.NewTestClusterFromClient(onboarding, env.Client(onboarding)))
}

// secretRequest builds a TypedRequest for a Secret with the given name and namespace.
func secretRequest(name, namespace string) controller.TypedRequest { //nolint:unparam
	return controller.TypedRequest{
		NamespacedName:   client.ObjectKey{Name: name, Namespace: namespace},
		GroupVersionKind: secretGVK,
	}
}

// listRUs fetches all ResourceUsage objects from the onboarding cluster.
func listRUs(env *testutils.ComplexEnvironment) *usagev1alpha1.ResourceUsageList {
	rul := &usagev1alpha1.ResourceUsageList{}
	ExpectWithOffset(1, env.Client(onboarding).List(env.Ctx, rul)).To(Succeed())
	return rul
}

var _ = Describe("Tracked Resource Controller", Serial, func() {

	BeforeEach(func() {
		resetSharedInformation()
	})

	It("should return an error if shared information is not yet initialized", func() {
		resetSharedInformation(skipInitialized)
		env := defaultTestSetup("testdata", "resource", "test-01")
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).To(HaveOccurred())
	})

	It("should create a new ongoing ResourceUsage when no prior one exists", func() {
		env := defaultTestSetup("testdata", "resource", "test-01")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		Expect(ru.Spec.Resource.Kind).To(Equal("Secret"))
		Expect(ru.Spec.Resource.Name).To(Equal("secret-01"))
		Expect(ru.Spec.Resource.Namespace).To(Equal("test"))
		Expect(ru.Spec.TrackingPeriod.Start).NotTo(BeNil())
		Expect(ru.Spec.TrackingPeriod.End).NotTo(BeNil())
		Expect(ru.Spec.Usage).To(HaveLen(1))
		Expect(ru.Spec.Usage[0].End).To(BeNil())
		Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseOngoing))
	})

	It("should create a new ongoing ResourceUsage with trait values when no prior one exists", func() {
		env := defaultTestSetup("testdata", "resource", "test-02")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseOngoing))
		Expect(ru.Spec.Traits).To(HaveKey("tier"))
		Expect(ru.Spec.Traits["tier"]).To(HaveLen(1))
		Expect(ru.Spec.Traits["tier"][0].Value.Raw).To(MatchJSON(`"premium"`))
		Expect(ru.Spec.Traits["tier"][0].Usage[0].End).To(BeNil())
	})

	It("should not modify an ongoing ResourceUsage when the resource is unchanged", func() {
		env := defaultTestSetup("testdata", "resource", "test-03")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		before := listRUs(env)
		Expect(before.Items).To(HaveLen(1))
		beforeRU := before.Items[0].DeepCopy()

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		after := listRUs(env)
		Expect(after.Items).To(HaveLen(1))
		Expect(after.Items[0].Spec).To(Equal(beforeRU.Spec))
		Expect(after.Items[0].Status).To(Equal(beforeRU.Status))
	})

	It("should update an ongoing ResourceUsage when a tracked trait has changed", func() {
		env := defaultTestSetup("testdata", "resource", "test-04")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		var standardEntry, premiumEntry *usagev1alpha1.TraitUsage
		for i := range ru.Spec.Traits["tier"] {
			switch string(ru.Spec.Traits["tier"][i].Value.Raw) {
			case `"standard"`:
				standardEntry = &ru.Spec.Traits["tier"][i]
			case `"premium"`:
				premiumEntry = &ru.Spec.Traits["tier"][i]
			}
		}
		Expect(standardEntry).NotTo(BeNil())
		Expect(standardEntry.Usage[0].End).NotTo(BeNil())
		Expect(premiumEntry).NotTo(BeNil())
		Expect(premiumEntry.Usage[0].End).To(BeNil())
	})

	It("should complete an expired ResourceUsage and create a new one", func() {
		env := defaultTestSetup("testdata", "resource", "test-05")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(2))

		var completed, ongoing *usagev1alpha1.ResourceUsage
		for i := range rul.Items {
			switch rul.Items[i].Status.Phase {
			case usagev1alpha1.UsagePhaseCompleted:
				completed = &rul.Items[i]
			case usagev1alpha1.UsagePhaseOngoing:
				ongoing = &rul.Items[i]
			}
		}
		Expect(completed).NotTo(BeNil())
		Expect(completed.Spec.Usage[0].End).NotTo(BeNil())
		Expect(completed.Status.TotalTrackedDuration).NotTo(BeNil())
		Expect(ongoing).NotTo(BeNil())
		Expect(ongoing.Spec.Usage[0].End).To(BeNil())
	})

	It("should stop tracking when the resource is deleted and the tracking period is still active", func() {
		env := defaultTestSetup("testdata", "resource", "test-06")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		// StopTracking closes the usage entry but does not complete the RU
		Expect(ru.Spec.Usage[0].End).NotTo(BeNil())
		Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseOngoing))
	})

	It("should complete the ResourceUsage when the resource is deleted and its tracking period has ended", func() {
		env := defaultTestSetup("testdata", "resource", "test-07")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseCompleted))
		Expect(ru.Spec.Usage[0].End).NotTo(BeNil())
		Expect(ru.Status.TotalTrackedDuration).NotTo(BeNil())
	})

	It("should do nothing when the resource is deleted and no ResourceUsage exists", func() {
		env := defaultTestSetup("testdata", "resource", "test-08")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		Expect(listRUs(env).Items).To(BeEmpty())
	})

	It("should do nothing when the resource is deleted and the ResourceUsage is already completed", func() {
		env := defaultTestSetup("testdata", "resource", "test-09")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		before := listRUs(env)
		Expect(before.Items).To(HaveLen(1))
		beforeRU := before.Items[0].DeepCopy()

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		after := listRUs(env)
		Expect(after.Items).To(HaveLen(1))
		Expect(after.Items[0].Spec).To(Equal(beforeRU.Spec))
		Expect(after.Items[0].Status).To(Equal(beforeRU.Status))
	})

	It("should stop tracking when the resource's GVK is no longer in the config", func() {
		env := defaultTestSetup("testdata", "resource", "test-10")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		Expect(rul.Items[0].Spec.Usage[0].End).NotTo(BeNil())
	})

	It("should stop tracking when the resource no longer matches the namespace selector", func() {
		env := defaultTestSetup("testdata", "resource", "test-11")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		Expect(rul.Items[0].Spec.Usage[0].End).NotTo(BeNil())
	})

	It("should stop tracking when the resource has a deletion timestamp and trackUntil is DeletionTimestamp", func() {
		env := defaultTestSetup("testdata", "resource", "test-12")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		secret := &corev1.Secret{}
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKey{Name: "secret-01", Namespace: "test"}, secret)).To(Succeed())
		Expect(env.Client(onboarding).Delete(env.Ctx, secret)).To(Succeed())
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKey{Name: "secret-01", Namespace: "test"}, secret)).To(Succeed())
		Expect(secret.DeletionTimestamp).NotTo(BeNil())

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		Expect(rul.Items[0].Spec.Usage[0].End).NotTo(BeNil())
	})

	It("should fetch the namespace and extract namespace traits correctly", func() {
		env := defaultTestSetup("testdata", "resource", "test-13")
		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		rc := newResourceController(env)

		_, err := rc.Reconcile(env.Ctx, secretRequest("secret-01", "test"))
		Expect(err).NotTo(HaveOccurred())

		rul := listRUs(env)
		Expect(rul.Items).To(HaveLen(1))
		ru := rul.Items[0]
		Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseOngoing))
		Expect(ru.Spec.Traits).To(HaveKey("project"))
		Expect(ru.Spec.Traits["project"][0].Value.Raw).To(MatchJSON(`"my-project"`))
	})

	It("should trigger reconciliation for all ongoing ResourceUsages on startup", func() {
		env := defaultTestSetup("testdata", "resource", "test-14")
		rc := newResourceController(env)

		err := rc.StartupReconciliation(env.Ctx)
		Expect(err).NotTo(HaveOccurred())

		// two Ongoing RUs → two triggers; Completed RU → no trigger
		triggered := drainReconcileTrigger()
		Expect(triggered).To(ConsistOf("test/secret-01", "test/secret-02"))
	})
})

var _ = Describe("RequeueAtTrackingPeriodEnd", func() {
	It("should return empty result for nil input", func() {
		result := controller.RequeueAtTrackingPeriodEnd(nil)
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should return empty result when tracking period end is zero", func() {
		ru := &usagev1alpha1.ResourceUsage{}
		result := controller.RequeueAtTrackingPeriodEnd(ru)
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should return empty result when tracking period end is in the past", func() {
		ru := &usagev1alpha1.ResourceUsage{}
		past := metav1.NewTime(time.Now().Add(-time.Hour))
		ru.Spec.TrackingPeriod.End = &past
		result := controller.RequeueAtTrackingPeriodEnd(ru)
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should return a positive requeue duration when tracking period end is in the future", func() {
		ru := &usagev1alpha1.ResourceUsage{}
		future := metav1.NewTime(time.Now().Add(time.Hour))
		ru.Spec.TrackingPeriod.End = &future
		result := controller.RequeueAtTrackingPeriodEnd(ru)
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})
})
