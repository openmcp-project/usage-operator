package usage_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/usage"
)

func loadResourceToTrack(folder string) *usagev1alpha1.ResourceToTrack {
	data, err := os.ReadFile(filepath.Join("testdata", folder, "config.yaml"))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	var cfg usagev1alpha1.UsageServiceConfig
	ExpectWithOffset(1, yaml.Unmarshal(data, &cfg)).NotTo(HaveOccurred())
	ExpectWithOffset(1, cfg.Spec.ResourcesToTrack).NotTo(BeEmpty())
	return &cfg.Spec.ResourcesToTrack[0]
}

func loadResourceUsage(folder string, filename ...string) *usagev1alpha1.ResourceUsage {
	name := "resource-usage.yaml"
	if len(filename) > 0 {
		name = filename[0]
	}
	data, err := os.ReadFile(filepath.Join("testdata", folder, name))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	var ru usagev1alpha1.ResourceUsage
	ExpectWithOffset(1, yaml.Unmarshal(data, &ru)).NotTo(HaveOccurred())
	return &ru
}

func newTestTracker(cfg *usagev1alpha1.ResourceToTrack) *usage.UsageTracker {
	te, err := usage.NewUsageTracker(context.Background(), cfg)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return te
}

func t(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return v
}

var _ = Describe("Tracking", func() {

	Context("NamespaceRequired", func() {

		It("should correctly mark the namespace as required if there is a non-nil namespace selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-01"))
			Expect(tracker.NamespaceRequired()).To(BeTrue())
		})

		It("should correctly mark the namespace as required if there is a trait defined which accesses the namespace", func() {
			tracker := newTestTracker(loadResourceToTrack("test-02"))
			Expect(tracker.NamespaceRequired()).To(BeTrue())
		})

		It("should correctly mark the namespace as not required if neither the selector nor any trait definition require it", func() {
			tracker := newTestTracker(loadResourceToTrack("test-03"))
			Expect(tracker.NamespaceRequired()).To(BeFalse())
		})

	})

	Context("MatchesSelector", func() {

		It("should match any object if no selector is configured", func() {
			tracker := newTestTracker(loadResourceToTrack("test-04"))
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("any", "any"), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})

		It("should match an object whose labels satisfy the resource selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-10"))
			obj := newTestObj("my-deployment", "default")
			obj.Labels = map[string]string{"track": "true"}
			matched, err := tracker.MatchesSelector(context.Background(), obj, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})

		It("should not match an object whose labels do not satisfy the resource selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-10"))
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "default"), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeFalse())
		})

		It("should match an object whose namespace name is in the namespace name selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-01"))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tracked-ns"}}
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "tracked-ns"), ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})

		It("should not match an object whose namespace name is not in the namespace name selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-01"))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other-ns"}}
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "other-ns"), ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeFalse())
		})

		It("should not match any object if the namespace names selector is an empty list", func() {
			tracker := newTestTracker(loadResourceToTrack("test-11"))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "any-ns"}}
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "any-ns"), ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeFalse())
		})

		It("should match an object whose namespace labels satisfy the namespace label selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-12"))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod-ns", Labels: map[string]string{"env": "production"}}}
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "prod-ns"), ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})

		It("should not match an object whose namespace labels do not satisfy the namespace label selector", func() {
			tracker := newTestTracker(loadResourceToTrack("test-12"))
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev-ns", Labels: map[string]string{"env": "development"}}}
			matched, err := tracker.MatchesSelector(context.Background(), newTestObj("my-deployment", "dev-ns"), ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeFalse())
		})

	})

	Context("NewResourceUsage", func() {

		It("should create a new ResourceUsage object with the correct fields (no traits)", func() {
			cfg := loadResourceToTrack("test-04")
			tracker := newTestTracker(cfg)
			now := t("2026-07-15T10:00:00Z")

			obj := newTestObj("my-deployment", "default")
			ru := tracker.NewResourceUsage(obj, nil, now)

			Expect(ru.Spec.Resource.Group).To(Equal(cfg.Group))
			Expect(ru.Spec.Resource.Version).To(Equal(cfg.Version))
			Expect(ru.Spec.Resource.Kind).To(Equal(cfg.Kind))
			Expect(ru.Spec.Resource.Name).To(Equal(obj.GetName()))
			Expect(ru.Spec.Resource.Namespace).To(Equal(obj.GetNamespace()))
			Expect(ru.Spec.TrackingPeriod.Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:00:00Z")))))
			Expect(ru.Spec.TrackingPeriod.End).To(Equal(new(metav1.NewTime(t("2026-07-15T11:00:00Z")))))
			Expect(ru.Spec.Usage).To(HaveLen(1))
			Expect(ru.Spec.Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:00:00Z")))))
			Expect(ru.Spec.Usage[0].End).To(BeNil())
			Expect(ru.Spec.Traits).To(BeEmpty())
		})

		It("should create a new ResourceUsage object with the correct fields (with traits)", func() {
			cfg := loadResourceToTrack("test-05")
			tracker := newTestTracker(cfg)
			now := t("2026-07-15T10:00:00Z")

			obj := newTestObj("my-deployment", "default")
			traitData := map[string][]byte{
				"replicas": []byte(`3`),
				"image":    []byte(`"nginx:1.25"`),
			}
			ru := tracker.NewResourceUsage(obj, traitData, now)

			Expect(ru.Spec.Traits).To(HaveLen(2))
			Expect(ru.Spec.Traits["replicas"]).To(HaveLen(1))
			Expect(ru.Spec.Traits["replicas"][0].Value.Raw).To(MatchJSON(`3`))
			Expect(ru.Spec.Traits["replicas"][0].Usage).To(HaveLen(1))
			Expect(ru.Spec.Traits["replicas"][0].Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:00:00Z")))))
			Expect(ru.Spec.Traits["replicas"][0].Usage[0].End).To(BeNil())
			Expect(ru.Spec.Traits["image"]).To(HaveLen(1))
			Expect(ru.Spec.Traits["image"][0].Value.Raw).To(MatchJSON(`"nginx:1.25"`))
		})

	})

	Context("CompleteResourceUsage", func() {

		It("should correctly complete a ResourceUsage object and set the end time", func() {
			ru := loadResourceUsage("test-04")
			var tracker *usage.UsageTracker // CompleteResourceUsage works on nil receiver
			tracker.CompleteResourceUsage(ru)

			Expect(ru.Spec.Usage[0].End).To(Equal(ru.Spec.TrackingPeriod.End))
			Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseCompleted))
			Expect(ru.Status.TotalTrackedDuration).NotTo(BeNil())
		})

		It("should correctly complete a ResourceUsage object and set the end time for traits", func() {
			ru := loadResourceUsage("test-04", "resource-usage-with-traits.yaml")
			var tracker *usage.UsageTracker
			tracker.CompleteResourceUsage(ru)

			Expect(ru.Spec.Usage[0].End).To(Equal(ru.Spec.TrackingPeriod.End))
			Expect(ru.Status.Phase).To(Equal(usagev1alpha1.UsagePhaseCompleted))
			Expect(ru.Spec.Traits).NotTo(BeEmpty())
			for traitName, traitUsages := range ru.Spec.Traits {
				Expect(traitUsages[0].Usage[0].End).To(Equal(ru.Spec.TrackingPeriod.End), "trait %q should have end time set to tracking period end", traitName)
			}
		})

	})

	Context("StopTracking", func() {

		It("should correctly stop tracking a ResourceUsage object and set the end time", func() {
			ru := loadResourceUsage("test-04")
			stopTime := t("2026-07-15T10:30:00Z")
			var tracker *usage.UsageTracker // StopTracking works on nil receiver
			tracker.StopTracking(ru, stopTime)

			Expect(ru.Spec.Usage[0].End).To(Equal(new(metav1.NewTime(t("2026-07-15T10:30:00Z")))))
			// StopTracking does not set status.phase — that is CompleteResourceUsage's job
			Expect(ru.Status.Phase).To(BeEmpty())
		})

		It("should correctly stop tracking a ResourceUsage object and set the end time for traits", func() {
			ru := loadResourceUsage("test-04", "resource-usage-with-traits.yaml")
			stopTime := t("2026-07-15T10:30:00Z")
			var tracker *usage.UsageTracker
			tracker.StopTracking(ru, stopTime)

			Expect(ru.Spec.Usage[0].End).To(Equal(new(metav1.NewTime(t("2026-07-15T10:30:00Z")))))
			Expect(ru.Spec.Traits).NotTo(BeEmpty())
			for traitName, traitUsages := range ru.Spec.Traits {
				Expect(traitUsages[0].Usage[0].End).To(Equal(new(metav1.NewTime(t("2026-07-15T10:30:00Z")))), "trait %q should have end time set to stop time", traitName)
			}
			Expect(ru.Status.Phase).To(BeEmpty())
		})

	})

	Context("Track", func() {

		It("should not modify the object if nothing changed", func() {
			cfg := loadResourceToTrack("test-06")
			tracker := newTestTracker(cfg)
			ru := loadResourceUsage("test-06")
			original := ru.DeepCopy()

			tracker.Track(ru, newTestObj("my-deployment", "default"), map[string][]byte{
				"replicas": []byte(`3`),
			}, t("2026-07-15T10:15:00Z"))

			Expect(ru.Spec.Usage).To(Equal(original.Spec.Usage))
			Expect(ru.Spec.Traits).To(Equal(original.Spec.Traits))
		})

		It("should correctly start tracking for a previously untracked object", func() {
			cfg := loadResourceToTrack("test-07")
			tracker := newTestTracker(cfg)
			ru := loadResourceUsage("test-07")
			now := t("2026-07-15T10:45:00Z")

			tracker.Track(ru, newTestObj("my-deployment", "default"), map[string][]byte{
				"replicas": []byte(`3`),
			}, now)

			// a new open usage entry should be prepended
			Expect(ru.Spec.Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:45:00Z")))))
			Expect(ru.Spec.Usage[0].End).To(BeNil())
			// the old closed entry is still there
			Expect(ru.Spec.Usage).To(HaveLen(2))
			// trait tracking resumes: a new open entry is prepended
			Expect(ru.Spec.Traits["replicas"][0].Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:45:00Z")))))
			Expect(ru.Spec.Traits["replicas"][0].Usage[0].End).To(BeNil())
		})

		It("should correctly create entries for previously untracked traits and stop tracking for traits which are no longer tracked", func() {
			cfg := loadResourceToTrack("test-08")
			tracker := newTestTracker(cfg)
			ru := loadResourceUsage("test-08")
			now := t("2026-07-15T10:20:00Z")

			tracker.Track(ru, newTestObj("my-deployment", "default"), map[string][]byte{
				"image": []byte(`"nginx:1.25"`),
			}, now)

			// 'replicas' is no longer in traitData — its open entry should be closed
			Expect(ru.Spec.Traits["replicas"][0].Usage[0].End).To(Equal(new(metav1.NewTime(t("2026-07-15T10:20:00Z")))))
			// 'image' is new — a fresh entry should be created
			Expect(ru.Spec.Traits["image"]).To(HaveLen(1))
			Expect(ru.Spec.Traits["image"][0].Value.Raw).To(MatchJSON(`"nginx:1.25"`))
			Expect(ru.Spec.Traits["image"][0].Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:20:00Z")))))
			Expect(ru.Spec.Traits["image"][0].Usage[0].End).To(BeNil())
		})

		It("should correctly update the entries for traits which have changed", func() {
			cfg := loadResourceToTrack("test-09")
			tracker := newTestTracker(cfg)
			ru := loadResourceUsage("test-09")
			now := t("2026-07-15T10:20:00Z")

			tracker.Track(ru, newTestObj("my-deployment", "default"), map[string][]byte{
				"replicas": []byte(`5`),
			}, now)

			// old value entry (3) should have its open usage closed
			oldIdx := -1
			newIdx := -1
			for i, tu := range ru.Spec.Traits["replicas"] {
				if string(tu.Value.Raw) == "3" {
					oldIdx = i
				}
				if string(tu.Value.Raw) == "5" {
					newIdx = i
				}
			}
			Expect(oldIdx).NotTo(Equal(-1))
			Expect(newIdx).NotTo(Equal(-1))
			Expect(ru.Spec.Traits["replicas"][oldIdx].Usage[0].End).To(Equal(new(metav1.NewTime(t("2026-07-15T10:20:00Z")))))
			// new value entry (5) should have an open usage starting now
			Expect(ru.Spec.Traits["replicas"][newIdx].Usage[0].Start).To(Equal(new(metav1.NewTime(t("2026-07-15T10:20:00Z")))))
			Expect(ru.Spec.Traits["replicas"][newIdx].Usage[0].End).To(BeNil())
		})

	})

})

// newTestObj returns a minimal client.Object for use in tracking tests.
func newTestObj(name, namespace string) *TestResource {
	return &TestResource{
		ObjectMeta: &metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}
