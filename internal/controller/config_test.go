package controller_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/shared"
)

var _ = Describe("Config Controller", Serial, func() {

	BeforeEach(func() {
		resetSharedInformation()
	})

	It("should set watches according to the config", func() {
		env := defaultTestSetup("testdata", "config", "test-01")

		req := testutils.RequestFromStrings("usage")
		rr := env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		// verify shared information state
		Expect(shared.SharedInformation().GetActiveInformers().UnsortedList()).To(ConsistOf(
			secretGVK,
			configMapGVK,
		))
		Expect(shared.SharedInformation().GetAllWatches()).To(HaveLen(2))
		secretTracker := shared.SharedInformation().GetWatch(secretGVK)
		Expect(secretTracker).ToNot(BeNil())
		Expect(secretTracker.Config.GroupVersionKind).To(BeEquivalentTo(secretGVK))
		Expect(secretTracker.Config.ResourceUsagePeriod).ToNot(BeNil())
		Expect(secretTracker.Config.ResourceUsagePeriod.Duration).To(BeEquivalentTo(72 * time.Hour))
		Expect(secretTracker.Config.TrackUntil).To(BeEquivalentTo(usagev1alpha1.TrackUntilDeletionTimestamp))
		Expect(secretTracker.Config.Traits).To(HaveLen(2))
		Expect(secretTracker.Config.Traits).To(HaveKeyWithValue("foo", usagev1alpha1.Trait{Path: ".namespace.metadata.labels.foo\\.bar\\.baz/foo"}))
		Expect(secretTracker.Config.Traits).To(HaveKeyWithValue("bar", usagev1alpha1.Trait{Path: ".namespace.metadata.labels.foo\\.bar\\.baz/bar"}))
		cmTracker := shared.SharedInformation().GetWatch(configMapGVK)
		Expect(cmTracker).ToNot(BeNil())
		Expect(cmTracker.Config.GroupVersionKind).To(BeEquivalentTo(configMapGVK))
		Expect(cmTracker.Config.ResourceUsagePeriod).ToNot(BeNil())
		Expect(cmTracker.Config.ResourceUsagePeriod.Duration).To(BeEquivalentTo(720 * time.Hour))
		Expect(cmTracker.Config.TrackUntil).To(BeEquivalentTo(usagev1alpha1.TrackUntilDeletion))
		Expect(cmTracker.Config.Traits).To(BeEmpty())

		// now we add one entry to the config and remove another one to verify that the watches are updated accordingly
		uscfg := &usagev1alpha1.UsageServiceConfig{}
		uscfg.SetName(req.Name)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(uscfg), uscfg)).To(Succeed())
		// remove 'bar' trait from first entry
		delete(uscfg.Spec.ResourcesToTrack[0].Traits, "bar")
		// modify second entry to match deployments instead of configmaps
		uscfg.Spec.ResourcesToTrack[1].Group = deploymentGVK.Group
		uscfg.Spec.ResourcesToTrack[1].Version = deploymentGVK.Version
		uscfg.Spec.ResourcesToTrack[1].Kind = deploymentGVK.Kind
		Expect(env.Client(platform).Update(env.Ctx, uscfg)).To(Succeed())

		// reconcile again
		rr = env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		// verify updated shared information state
		Expect(shared.SharedInformation().GetActiveInformers().UnsortedList()).To(ConsistOf(
			secretGVK,
			configMapGVK, // this is only removed when the config is reset
			deploymentGVK,
		))
		Expect(shared.SharedInformation().GetAllWatches()).To(HaveLen(2))
		secretTracker = shared.SharedInformation().GetWatch(secretGVK)
		Expect(secretTracker).ToNot(BeNil())
		Expect(secretTracker.Config.GroupVersionKind).To(BeEquivalentTo(secretGVK))
		Expect(secretTracker.Config.Traits).To(HaveLen(1))
		Expect(secretTracker.Config.Traits).To(HaveKeyWithValue("foo", usagev1alpha1.Trait{Path: ".namespace.metadata.labels.foo\\.bar\\.baz/foo"}))

		deploymentTracker := shared.SharedInformation().GetWatch(deploymentGVK)
		Expect(deploymentTracker).ToNot(BeNil())
		Expect(deploymentTracker.Config.GroupVersionKind).To(BeEquivalentTo(deploymentGVK))
		Expect(deploymentTracker.Config.Traits).To(BeEmpty())

		cmTracker = shared.SharedInformation().GetWatch(configMapGVK)
		Expect(cmTracker).To(BeNil())
	})

	It("should clear all watches if the config does not exist", func() {
		env := defaultTestSetup("testdata", "config", "test-01")

		req := testutils.RequestFromStrings("usage")
		rr := env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(shared.SharedInformation().GetAllWatches()).To(HaveLen(2))

		// delete the config
		uscfg := &usagev1alpha1.UsageServiceConfig{}
		uscfg.SetName(req.Name)
		Expect(env.Client(platform).Delete(env.Ctx, uscfg)).To(Succeed())

		// reconcile again
		rr = env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		// verify that all watches have been cleared
		Expect(shared.SharedInformation().GetAllWatches()).To(BeEmpty())
	})

	It("should clear all watches if the config is in deletion", func() {
		env := defaultTestSetup("testdata", "config", "test-01")

		req := testutils.RequestFromStrings("usage")
		rr := env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(shared.SharedInformation().GetAllWatches()).To(HaveLen(2))

		// add a finalizer to prevent immediate deletion
		uscfg := &usagev1alpha1.UsageServiceConfig{}
		uscfg.SetName(req.Name)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(uscfg), uscfg)).To(Succeed())
		uscfg.SetFinalizers([]string{"dummy"})
		Expect(env.Client(platform).Update(env.Ctx, uscfg)).To(Succeed())

		// delete the config (will be stuck in deletion due to the finalizer)
		Expect(env.Client(platform).Delete(env.Ctx, uscfg)).To(Succeed())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(uscfg), uscfg)).To(Succeed())
		Expect(uscfg.DeletionTimestamp).ToNot(BeNil())

		// reconcile again
		rr = env.ShouldReconcile(cfgRec, req)
		Expect(rr.RequeueAfter).To(BeZero())

		// verify that all watches have been cleared
		Expect(shared.SharedInformation().GetAllWatches()).To(BeEmpty())
	})

})
