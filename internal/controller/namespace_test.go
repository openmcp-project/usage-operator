package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
)

var _ = Describe("Namespace Controller", Serial, func() {

	BeforeEach(func() {
		resetSharedInformation()
	})

	It("should trigger reconciliations for resources whose config has a namespace trait", func() {
		// Secrets have a .namespace trait, ConfigMaps do not → only Secrets triggered.
		// secret-03 lives in namespace 'other' and must not be triggered.
		env := defaultTestSetup("testdata", "namespace", "test-01")

		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		drainReconcileTrigger()

		rr := env.ShouldReconcile(nsRec, testutils.RequestFromStrings("test"))
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(drainReconcileTrigger()).To(ConsistOf(
			"test/secret-01", "test/secret-02",
		))
	})

	It("should trigger reconciliations for resources whose config has a namespace selector", func() {
		// Secrets have a namespace selector, ConfigMaps do not → only Secrets triggered
		env := defaultTestSetup("testdata", "namespace", "test-02")

		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		drainReconcileTrigger()

		rr := env.ShouldReconcile(nsRec, testutils.RequestFromStrings("test"))
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(drainReconcileTrigger()).To(ConsistOf(
			"test/secret-01", "test/secret-02",
		))
	})

	It("should not trigger any reconciliations if the namespace contains no tracked resources", func() {
		// Secrets have a namespace selector, but the namespace is empty
		env := defaultTestSetup("testdata", "namespace", "test-03")

		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		drainReconcileTrigger()

		rr := env.ShouldReconcile(nsRec, testutils.RequestFromStrings("test"))
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(drainReconcileTrigger()).To(BeEmpty())
	})

	It("should not trigger any reconciliations if no tracked resource type requires the namespace", func() {
		// Only ConfigMaps are tracked, which have no namespace trait or selector
		env := defaultTestSetup("testdata", "namespace", "test-04")

		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		drainReconcileTrigger()

		rr := env.ShouldReconcile(nsRec, testutils.RequestFromStrings("test"))
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(drainReconcileTrigger()).To(BeEmpty())
	})

	It("should return no error if the namespace does not exist", func() {
		env := defaultTestSetup("testdata", "namespace", "test-01")

		env.ShouldReconcile(cfgRec, testutils.RequestFromStrings(providerName))
		drainReconcileTrigger()

		rr := env.ShouldReconcile(nsRec, testutils.RequestFromStrings("nonexistent"))
		Expect(rr.RequeueAfter).To(BeZero())

		Expect(drainReconcileTrigger()).To(BeEmpty())
	})
})
