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

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/shared"
)

const (
	GarbageCollectorName = "GarbageCollector"
)

// GarbageCollector runs periodically and checks for ResourceUsage objects that should be deleted according to the configured garbage collection policy.
type GarbageCollector struct {
	OnboardingCluster *clusters.Cluster
	interval          time.Duration
	trigger           chan struct{}
	lastRun           time.Time
	ticker            *time.Ticker
}

func NewGarbageCollector(onboardingCluster *clusters.Cluster) *GarbageCollector {
	return &GarbageCollector{
		OnboardingCluster: onboardingCluster,
		interval:          usagev1alpha1.DefaultGarbageCollectionInterval,
		trigger:           make(chan struct{}, 5),               // buffered channel to avoid blocking if multiple triggers are sent in quick succession
		ticker:            time.NewTicker(365 * 24 * time.Hour), // initial ticker, will be reset to the configured interval on first run
	}
}

func (c *GarbageCollector) setNextRun(log logging.Logger, maybeInterval ...time.Duration) {
	interval := c.interval
	if len(maybeInterval) > 0 {
		interval = maybeInterval[0]
	}
	c.ticker.Reset(interval)
	log.Debug("Scheduling next garbage collection run", "time", time.Now().Add(interval).Format(time.RFC3339))
}

func (c *GarbageCollector) getConfig() *usagev1alpha1.GarbageCollectionConfig {
	cfg := shared.SharedInformation().GetGarbageCollectionConfig()
	c.interval = cfg.GetInterval()
	return cfg
}

// GetTrigger returns the channel that triggers the garbage collector to check for a new configuration, potentially triggering a garbage collection run.
// Mainly exposed for testing purposes, should not required to be called in production code, as the garbage collector will inject this into the shared information on startup.
func (c *GarbageCollector) GetTrigger() chan struct{} {
	return c.trigger
}

// Start makes the GarbageCollector implement the Runnable interface, so it can be started by the controller-runtime manager.
func (c *GarbageCollector) Start(ctx context.Context) error {
	// controller-runtime does not inject a logger here, since this is not a controller, so we need to create one ourselves
	log, err := logging.GetLogger()
	if err != nil {
		return fmt.Errorf("unable to create or get logger: %w", err)
	}
	log = log.WithName(GarbageCollectorName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting GarbageCollector runnable")

	runImmediately := shared.SharedInformation().SetGarbageCollectionTrigger(c.trigger)
	if runImmediately {
		// run garbage collection immediately if the trigger was used before the channel was set up
		// this will also adjust the interval
		c.CollectGarbage(ctx, time.Now())
		c.setNextRun(log)
	}

	defer c.ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.ticker.C:
			c.CollectGarbage(ctx, time.Now())
			c.setNextRun(log)
		case <-c.trigger:
			// this happens when the configuration was (re-)loaded, potentially changing the garbage collection configuration
			// compute when the next GarbageCollection run should happen, based on the configured interval and the last run time
			_ = c.getConfig() // update interval based on the current configuration
			// due to the zero time being very long ago, this will always trigger a garbage collection run on the first trigger
			nextRun := c.lastRun.Add(c.interval)
			if !nextRun.After(time.Now()) {
				// if the next run is in the past (or now), run garbage collection immediately
				c.CollectGarbage(ctx, time.Now())
				c.setNextRun(log)
			} else {
				// compute the duration until the next run and reset the ticker accordingly
				c.setNextRun(log, time.Until(nextRun))
			}
		}
	}
}

// CollectGarbage runs the garbage collection process.
// It checks for ResourceUsage objects which are completed and obsolete (according to the garbage collection config) and deletes them.
// Errors are simply logged, as the garbage collection will be retried on the next run.
// The 'now' argument is mainly for testing, it should always be set to time.Now() in production code.
func (c *GarbageCollector) CollectGarbage(ctx context.Context, now time.Time) {
	log := logging.FromContextOrPanic(ctx)

	// get current garbage collection configuration
	gcc := c.getConfig()
	if gcc == nil || gcc.KeepCount == 0 && gcc.KeepDuration == nil {
		log.Info("No garbage collection configured, skipping garbage collection")
		return
	}

	log.Info("Starting garbage collection")
	c.lastRun = now

	// list all completed ResourceUsage objects
	rus := &usagev1alpha1.ResourceUsageList{}
	if err := c.OnboardingCluster.Client().List(ctx, rus, client.MatchingFields{"status.phase": "Completed"}); err != nil {
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
			if err := c.OnboardingCluster.Client().Delete(ctx, ru); err != nil {
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
