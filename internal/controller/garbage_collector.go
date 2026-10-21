package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/periodic"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/shared"
)

const (
	GarbageCollectorName = "GarbageCollector"
)

func NewGarbageCollector(onboardingCluster *clusters.Cluster) (periodic.Periodic[*usagev1alpha1.GarbageCollectionConfig], error) {
	p, err := periodic.NewPeriodicBuilder(generateGarbageCollectionFunction(onboardingCluster)).
		WithName(GarbageCollectorName).
		WithInterval(5 * time.Second). // initial interval, will be updated on the first run
		WithConfigFuncs(func(ctx context.Context, cfg *usagev1alpha1.GarbageCollectionConfig) (*usagev1alpha1.GarbageCollectionConfig, error) {
			if !shared.SharedInformation().IsInitialized() {
				// returning an error while the config is not available causes the garbage collection to be aborted
				// the small initial interval configured above causes frequent retries until the config is available
				return nil, fmt.Errorf("waiting for the garbage collection config to be loaded")
			}
			return shared.SharedInformation().GetGarbageCollectionConfig(), nil
		}).
		Build()
	if err != nil {
		return nil, err
	}
	shared.SharedInformation().SetGarbageCollectionTrigger(p.GetTrigger())
	return p, nil
}

func generateGarbageCollectionFunction(onboardingCluster *clusters.Cluster) periodic.RunLogicFunc[*usagev1alpha1.GarbageCollectionConfig] {
	return func(ctx context.Context, p periodic.Periodic[*usagev1alpha1.GarbageCollectionConfig], gcc *usagev1alpha1.GarbageCollectionConfig) {
		log := p.Log()
		CollectGarbage(ctx, gcc, onboardingCluster, log, time.Now())
		oldInterval := p.GetInterval()
		newInterval := shared.SharedInformation().GetGarbageCollectionConfig().GetInterval()
		if oldInterval != newInterval {
			p.SetInterval(newInterval)
		}
	}
}

// CollectGarbage runs the garbage collection process.
// It checks for ResourceUsage objects which are completed and obsolete (according to the garbage collection config) and deletes them.
// Errors are simply logged, as the garbage collection will be retried on the next run.
// The 'now' argument is mainly for testing, it should always be set to time.Now() in production code.
func CollectGarbage(ctx context.Context, gcc *usagev1alpha1.GarbageCollectionConfig, onboardingCluster *clusters.Cluster, log logging.Logger, now time.Time) {
	if gcc == nil || gcc.KeepCount == 0 && gcc.KeepDuration == nil {
		log.Info("No garbage collection configured, skipping garbage collection")
		return
	}
	log.Info("Starting garbage collection")

	// list all completed ResourceUsage objects
	rus := &usagev1alpha1.ResourceUsageList{}
	if err := onboardingCluster.Client().List(ctx, rus, client.MatchingFields{"status.phase": "Completed"}); err != nil {
		log.Error(err, "Error listing ResourceUsage objects")
		return
	}
	mapped := map[string]*usagev1alpha1.ResourceUsage{}
	for i := range rus.Items {
		ru := &rus.Items[i]
		if ru.Spec.TrackingPeriod.End != nil {
			// ignore ResourceUsage objects without an end time (should not happen)
			mapped[ru.Name] = ru
		}
	}

	deleteByKeepCount := sets.New[string]()
	if gcc.KeepCount > 0 {
		// group ResourceUsage objects by referenced resource
		groupedUsage := map[usagev1alpha1.ResourceReference][]*usagev1alpha1.ResourceUsage{}
		for _, ru := range mapped {
			ref := ru.Spec.Resource
			groupedUsage[ref] = append(groupedUsage[ref], ru)
		}

		// sort each group by end time of the tracking period (most recent end time first)
		// ignore groups with less than or equal to KeepCount ResourceUsage objects
		// then, add all ResourceUsage objects that are older than the KeepCount-th newest to the deleteByKeepCount set
		for ref, usages := range groupedUsage {
			if len(usages) <= gcc.KeepCount {
				delete(groupedUsage, ref)
				continue
			}
			slices.SortStableFunc(usages, func(a, b *usagev1alpha1.ResourceUsage) int {
				return b.Spec.TrackingPeriod.End.Compare(a.Spec.TrackingPeriod.End.Time)
			})
			for _, ru := range usages[gcc.KeepCount:] {
				deleteByKeepCount.Insert(ru.Name)
			}
		}
	}

	deleteByKeepDuration := sets.New[string]()
	if gcc.KeepDuration != nil {
		for _, ru := range mapped {
			if now.Sub(ru.Spec.TrackingPeriod.End.Time) > gcc.KeepDuration.Duration {
				deleteByKeepDuration.Insert(ru.Name)
			}
		}
	}

	var toDelete sets.Set[string]
	if gcc.KeepCount > 0 && gcc.KeepDuration != nil && gcc.AndConditions {
		toDelete = deleteByKeepCount.Intersection(deleteByKeepDuration)
	} else {
		toDelete = deleteByKeepCount.Union(deleteByKeepDuration)
	}

	log.Info("Deleting ResourceUsage objects", "count", toDelete.Len())
	successCount := 0
	failCount := 0
	for name := range toDelete {
		if ru, ok := mapped[name]; ok {
			if err := onboardingCluster.Client().Delete(ctx, ru); err != nil {
				log.Error(err, "Error deleting ResourceUsage", "name", ru.Name, "group", ru.Spec.Resource.Group, "version", ru.Spec.Resource.Version, "kind", ru.Spec.Resource.Kind, "resource", ru.Spec.Resource.NamespacedName().String())
				failCount++
			} else {
				log.Debug("Deleted ResourceUsage", "name", ru.Name, "group", ru.Spec.Resource.Group, "version", ru.Spec.Resource.Version, "kind", ru.Spec.Resource.Kind, "resource", ru.Spec.Resource.NamespacedName().String())
				successCount++
			}
		}
	}

	log.Info("Finished garbage collection", "successfulDeletions", successCount, "failedDeletions", failCount)
}
