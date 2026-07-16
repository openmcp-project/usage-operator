package shared

import (
	"fmt"
	"reflect"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/openmcp-project/usage-operator/internal/usage"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
)

var sharedInstance *sharedInformation

// sharedInformation contains information which is shared between multiple controllers.
// All access to it should happen via its methods, which need to be thread-safe.
type sharedInformation struct {
	lock                           *sync.RWMutex
	watchedResources               map[schema.GroupVersionKind]*usage.UsageTracker
	activeInformers                sets.Set[schema.GroupVersionKind] // needs to be tracked separate from watchedResources, as informers cannot be stopped once started
	startInformer                  func(gvk schema.GroupVersionKind) error
	reconcileTrigger               chan event.TypedGenericEvent[*unstructured.Unstructured]
	garbageCollectionConfig        *usagev1alpha1.GarbageCollectionConfig
	garbageCollectionTrigger       chan struct{} // informs the garbage collector about a changed garbage collection configuration, also triggering a garbage collection run
	missedGarbageCollectionTrigger bool          // indicates that the garbage collection trigger was used before being setup
	initialized                    bool          // will be true if the config controller has reconciled at least once since the last restart of the operator
}

func SharedInformation() *sharedInformation {
	if sharedInstance == nil {
		sharedInstance = &sharedInformation{
			lock:             &sync.RWMutex{},
			watchedResources: map[schema.GroupVersionKind]*usage.UsageTracker{},
			activeInformers:  sets.New[schema.GroupVersionKind](),
		}
	}
	return sharedInstance
}

// SetGarbageCollectionConfig sets the garbage collection configuration.
// It is called by the config controller, when the configuration is reconciled.
func (si *sharedInformation) SetGarbageCollectionConfig(cfg *usagev1alpha1.GarbageCollectionConfig) {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.garbageCollectionConfig = cfg
	if si.garbageCollectionTrigger != nil {
		si.garbageCollectionTrigger <- struct{}{}
	} else {
		si.missedGarbageCollectionTrigger = true
	}
}

// GetGarbageCollectionConfig returns a deep copy of the current garbage collection configuration.
func (si *sharedInformation) GetGarbageCollectionConfig() *usagev1alpha1.GarbageCollectionConfig {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.garbageCollectionConfig.DeepCopy()
}

// SetWatch sets or removes a watch for the given resource type (GVK) and its associated UsageTracker.
// If tracker is nil, the watch will be removed. Otherwise, it will be set to the given tracker.
// If the first return value is true, this means that the caller needs to reconcile all objects of the given GVK in order to correctly handle the configuration change.
func (si *sharedInformation) SetWatch(gvk schema.GroupVersionKind, tracker *usage.UsageTracker) (bool, error) {
	si.lock.Lock()
	defer si.lock.Unlock()

	reconcileRequired := false
	if tracker == nil {
		// reconciliation is required if a watch was actually removed
		// because we need to stop tracking all resources of this GVK
		_, exists := si.watchedResources[gvk]
		reconcileRequired = reconcileRequired || exists
		delete(si.watchedResources, gvk)
	} else {
		if !si.activeInformers.Has(gvk) {
			// reconciliation is not required, because starting a new informer will automatically trigger a reconcile for all existing objects of the given GVK
			if si.startInformer == nil {
				return false, fmt.Errorf("startInformer function is not set")
			} else if err := si.startInformer(gvk); err != nil {
				return false, fmt.Errorf("error starting informer for %s: %w", gvk.String(), err)
			}
			si.activeInformers.Insert(gvk)
		} else {
			// reconciliation is required if the configuration has changed (either newly added or modified), but the informer is already active
			old, exists := si.watchedResources[gvk]
			reconcileRequired = reconcileRequired || !exists || !reflect.DeepEqual(old.Config, tracker.Config)
		}
		si.watchedResources[gvk] = tracker

	}
	return reconcileRequired, nil
}

// GetWatch retrieves the UsageTracker associated with the given resource type (GVK).
// It returns nil if no watch is set for the specified GVK.
func (si *sharedInformation) GetWatch(gvk schema.GroupVersionKind) *usage.UsageTracker {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.watchedResources[gvk]
}

// GetWatchesForNamespaceUpdate returns a slice which contains all UsageTrackers where an update to a namespace could be relevant
// (GVKs where either a namespace selector is set or any trait refers to the namespace).
func (si *sharedInformation) GetWatchesForNamespaceUpdate() []*usage.UsageTracker {
	si.lock.RLock()
	defer si.lock.RUnlock()

	trackers := make([]*usage.UsageTracker, 0, len(si.watchedResources))
	for _, tracker := range si.watchedResources {
		if tracker.NamespaceRequired() {
			trackers = append(trackers, tracker)
		}
	}
	return trackers
}

// ClearWatches removes all watches from the shared information.
func (si *sharedInformation) ClearWatches() {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.watchedResources = map[schema.GroupVersionKind]*usage.UsageTracker{}
}

// WatchedGVKs returns a set of all GroupVersionKinds (GVKs) that are currently being watched.
func (si *sharedInformation) WatchedGVKs() sets.Set[schema.GroupVersionKind] {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return sets.KeySet(si.watchedResources)
}

// SetStartInformerFunc registers the function to start informers for specific GVKs.
// This should be called once before any watches are set.
func (si *sharedInformation) SetStartInformerFunc(startInformer func(gvk schema.GroupVersionKind) error) {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.startInformer = startInformer
}

// SetReconcileTrigger sets the channel which will be used to trigger an object's reconiliation 'manually'.
// This should be called once at the beginning of the operator's lifecycle.
func (si *sharedInformation) SetReconcileTrigger(trigger chan event.TypedGenericEvent[*unstructured.Unstructured]) {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.reconcileTrigger = trigger
}

// TriggerReconcile can be used to trigger a reconcile for the given object.
// The object's GVK must be set.
// No effect if SetReconcileTrigger has not been called before.
func (si *sharedInformation) TriggerReconcile(obj *unstructured.Unstructured) {
	si.lock.RLock()
	defer si.lock.RUnlock()

	if si.reconcileTrigger != nil {
		si.reconcileTrigger <- event.TypedGenericEvent[*unstructured.Unstructured]{
			Object: obj,
		}
	}
}

// SetGarbageCollectionTrigger sets the channel which will be used to inform the garbage collector about a changed garbage collection configuration.
// This should be called once at the beginning of the operator's lifecycle.
// If SetGarbageCollectionConfig was called before this method, it will return true to indicate that the garbage collector should immediately check its config.
func (si *sharedInformation) SetGarbageCollectionTrigger(trigger chan struct{}) bool {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.garbageCollectionTrigger = trigger
	return si.missedGarbageCollectionTrigger
}

func (si *sharedInformation) SetInitialized() {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.initialized = true
}

// IsInitialized returns true if the config controller has reconciled successfully at least once since the last restart of the operator.
// This is used to prevent the resource controllers from reconciling resources before the config has been read,
// in which case they would incorrectly assume that no resources are being tracked and stop tracking all resources.
func (si *sharedInformation) IsInitialized() bool {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.initialized
}
