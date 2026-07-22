package controller_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openmcp-project/controller-utils/pkg/clusters"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/controller"
	"github.com/openmcp-project/usage-operator/internal/shared"
)

var (
	gcNow = time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
)

// test-01 contains two tracked resources ('foo' and 'bar' in namespace 'test'), each with 5 Completed
// ResourceUsage objects (ru-{foo,bar}-01 through -05) and one Ongoing object (ru-{foo,bar}-06).
// End times of the Completed objects:
//   ru-*-01: 2026-01-02  (13d12h before gcNow)
//   ru-*-02: 2026-01-03  (12d12h before gcNow)
//   ru-*-03: 2026-01-04  (11d12h before gcNow)
//   ru-*-04: 2026-01-05  (10d12h before gcNow)
//   ru-*-05: 2026-01-06  (9d12h  before gcNow)
//
// KeepCount=3 should keep the 3 newest completed objects (03,04,05) and delete the 2 oldest (01,02).
// KeepDuration=10d deletes everything whose end is more than 10 days before gcNow, i.e. 01–04
// (only 05 at 9d12h survives).

var _ = DescribeTable("Garbage Collection",
	Serial,
	func(gcCfg *usagev1alpha1.GarbageCollectionConfig, now time.Time, expectedRemainingResourceUsages sets.Set[string], testDirPathSegments ...string) {
		resetSharedInformation()
		env := defaultTestSetup(testDirPathSegments...)
		gc := controller.NewGarbageCollector(clusters.NewTestClusterFromClient(onboarding, env.Client(onboarding)))
		shared.SharedInformation().SetGarbageCollectionConfig(gcCfg)

		gc.CollectGarbage(env.Ctx, now)
		shared.SharedInformation().SetGarbageCollectionTrigger(gc.GetTrigger())

		// verify that the remaining ResourceUsage objects are as expected
		rus := &usagev1alpha1.ResourceUsageList{}
		Expect(env.Client(onboarding).List(env.Ctx, rus)).To(Succeed())
		actualRemainingResourceUsages := sets.New[string]()
		for _, ru := range rus.Items {
			actualRemainingResourceUsages.Insert(ru.Name)
		}
		Expect(sets.List(actualRemainingResourceUsages)).To(Equal(sets.List(expectedRemainingResourceUsages)))
	},
	// nil config: no garbage collection
	Entry("nil config - no garbage collection",
		nil,
		gcNow,
		sets.New("ru-foo-01", "ru-foo-02", "ru-foo-03", "ru-foo-04", "ru-foo-05", "ru-foo-06",
			"ru-bar-01", "ru-bar-02", "ru-bar-03", "ru-bar-04", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
	// empty config (KeepCount=0, KeepDuration=nil): no garbage collection
	Entry("empty config - no garbage collection",
		&usagev1alpha1.GarbageCollectionConfig{},
		gcNow,
		sets.New("ru-foo-01", "ru-foo-02", "ru-foo-03", "ru-foo-04", "ru-foo-05", "ru-foo-06",
			"ru-bar-01", "ru-bar-02", "ru-bar-03", "ru-bar-04", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
	// only KeepDuration=10d: deletes 01-04 of each resource (end times 13d12h–10d12h before gcNow)
	Entry("only KeepDuration - deletes objects older than the duration",
		&usagev1alpha1.GarbageCollectionConfig{
			KeepDuration: &metav1.Duration{Duration: 10 * 24 * time.Hour},
		},
		gcNow,
		sets.New("ru-foo-05", "ru-foo-06", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
	// only KeepCount=3: keeps the 3 newest completed objects (03,04,05), deletes the 2 oldest (01,02)
	Entry("only KeepCount - deletes oldest objects beyond the count",
		&usagev1alpha1.GarbageCollectionConfig{
			KeepCount: 3,
		},
		gcNow,
		sets.New("ru-foo-03", "ru-foo-04", "ru-foo-05", "ru-foo-06",
			"ru-bar-03", "ru-bar-04", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
	// KeepDuration=10d + KeepCount=3, AndConditions=false (OR): union of both deletion sets
	// KeepCount deletes {01,02}, KeepDuration deletes {01,02,03,04} → union = {01,02,03,04}
	Entry("KeepDuration + KeepCount (OR)",
		&usagev1alpha1.GarbageCollectionConfig{
			KeepDuration:  &metav1.Duration{Duration: 10 * 24 * time.Hour},
			KeepCount:     3,
			AndConditions: false,
		},
		gcNow,
		sets.New("ru-foo-05", "ru-foo-06", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
	// KeepDuration=10d + KeepCount=3, AndConditions=true (AND): intersection of both deletion sets
	// KeepCount deletes {01,02}, KeepDuration deletes {01,02,03,04} → intersection = {01,02}
	Entry("KeepDuration + KeepCount (AND)",
		&usagev1alpha1.GarbageCollectionConfig{
			KeepDuration:  &metav1.Duration{Duration: 10 * 24 * time.Hour},
			KeepCount:     3,
			AndConditions: true,
		},
		gcNow,
		sets.New("ru-foo-03", "ru-foo-04", "ru-foo-05", "ru-foo-06",
			"ru-bar-03", "ru-bar-04", "ru-bar-05", "ru-bar-06"),
		"testdata", "garbage_collector", "test-01",
	),
)
