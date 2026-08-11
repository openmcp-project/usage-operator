package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultGarbageCollectionInterval = 24 * time.Hour
)

type UsageServiceConfigSpec struct {
	// GarbageCollection specifies how usage records should be garbage collected.
	// +optional
	GarbageCollection *GarbageCollectionConfig `json:"garbageCollection,omitempty"`

	// ResourcesToTrack specifies for which resources usage should be tracked.
	ResourcesToTrack []ResourceToTrack `json:"resourcesToTrack,omitempty"`
}

type GarbageCollectionConfig struct {
	// KeepDuration specifies how long a ResourceUsage record should be kept after its tracking period has ended.
	// If not specified, no completed ResourceUsage records will be deleted based on their age.
	// +optional
	KeepDuration *metav1.Duration `json:"keepDuration,omitempty"`

	// KeepCount specifies how many completed ResourceUsage records (per resource) should be kept, regardless of their age.
	// If 0, no completed ResourceUsage records will be deleted based on their count.
	// +optional
	KeepCount int `json:"keepCount,omitempty"`

	// AndConditions specifies whether a completed ResourceUsage record should be deleted only if both the KeepDuration and KeepCount conditions are met (true), or if either of them is met (false).
	// If not specified, the default is false (delete if either condition is met).
	// Has no effect if only one of the conditions is specified.
	// +optional
	AndConditions bool `json:"andConditions,omitempty"`

	// Interval specifies the interval at which the garbage collector runs.
	// Defaults to 24 hours if not specified.
	// +kubebuilder:default="24h"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`
}

type ResourceToTrack struct {
	metav1.GroupVersionKind `json:",inline"`

	// Selector allows to filter the tracked resources.
	// +optional
	Selector *Selector `json:"selector,omitempty"`

	// ResourceUsagePeriod specifies the duration which should be tracked in a single ResourceUsage record for this resource type.
	// If the duration is exceeded, a new ResourceUsage record will be created for the resource.
	// Defaults to 30 days if not specified.
	// +kubebuilder:default="720h"
	// +optional
	ResourceUsagePeriod *metav1.Duration `json:"resourceUsagePeriod,omitempty"`

	// TrackUntil specifies when usage tracking is stopped.
	// This can either happen when the resource gets a deletion timestamp, or when it is actually removed.
	// Valid values are 'Deletion' and 'DeletionTimestamp', defaulting to the former one if not specified.
	// +kubebuilder:validation:Enum=Deletion;DeletionTimestamp
	// +optional
	TrackUntil TrackUntilMode `json:"trackUntil,omitempty"`

	// Traits specifies traits which should be tracked for the resource.
	// Each trait has an identifier (the key) and specifies the path to the field within the resource's manifest which should be tracked.
	// A trait's path can either refer to the resource itself (path starts with 'resource.') or to its namespace (if it is namespaced, path starts with 'namespace.').
	// Tracking a trait causes the trait's value to be stored the ResourceUsage record for the resource.
	// If the trait changes, all of its values and the time of the change will be stored in the ResourceUsage record.
	// Note that this can make the ResourceUsage record grow large, so it should only be used for fields which change rarely.
	// +optional
	Traits map[string]Trait `json:"traits,omitempty"`
}

// Default sets default values in the receiver object and returns it for chaining.
func (rtt *ResourceToTrack) Default() *ResourceToTrack {
	if rtt.ResourceUsagePeriod == nil {
		rtt.ResourceUsagePeriod = &metav1.Duration{Duration: 30 * 24 * time.Hour}
	}
	if rtt.TrackUntil == "" {
		rtt.TrackUntil = TrackUntilDeletion
	}
	return rtt
}

type TrackUntilMode string

const (
	// TrackUntilDeletion means that usage tracking is stopped when the resource is deleted.
	TrackUntilDeletion TrackUntilMode = "Deletion"
	// TrackUntilDeletionTimestamp means that usage tracking is stopped when the resource gets a deletion timestamp.
	TrackUntilDeletionTimestamp TrackUntilMode = "DeletionTimestamp"
)

type Selector struct {
	// ResourceSelector is a label selector for the resource itself. Only resources matching this selector will be tracked.
	// +optional
	ResourceSelector *metav1.LabelSelector `json:"resource,omitempty"`

	// NamespaceSelector allows to restrict the tracked resources to certain namespaces.
	// Namespaces can be selected by their labels, or by their names. Only the names are evaluated if both are specified.
	// DO NOT specify this in case of cluster-scoped resources, as it will cause errors when trying to fetch the namespace of the resource.
	// +optional
	NamespaceSelector *NamespaceSelector `json:"namespace,omitempty"`
}

type NamespaceSelector struct {
	// Names is a list of namespace names to select. Only resources in these namespaces will be tracked.
	// If this is specified, the LabelSelector in Selector will be ignored.
	// Note that there is a difference between a null and an empty list: a null value means that all namespaces are selected, while an empty list means that no namespaces are selected.
	// +nullable
	// +optional
	Names []string `json:"names,omitempty"`

	// Selector is a label selector for the namespace. Only resources in namespaces matching this selector will be tracked.
	// Will be ignored if specified together with Names.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type Trait struct {
	// Path is the path to the trait's field within the resource's manifest.
	// It must follow the JSONPath syntax, e.g. "resource.status.ready" or "resource.metadata.labels['foo']",
	// see http://goessner.net/articles/JsonPath/ for more information.
	// Note that the path will automatically be wrapped in curly braces, so it should not include them.
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=usc;uscfg
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
type UsageServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec UsageServiceConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
type UsageServiceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UsageServiceConfig `json:"items"`
}

func init() {
	RegisterToSchemeBuilder(&UsageServiceConfig{}, &UsageServiceConfigList{})
}

func (gcc *GarbageCollectionConfig) GetInterval() time.Duration {
	if gcc != nil && gcc.Interval != nil {
		return gcc.Interval.Duration
	}
	return DefaultGarbageCollectionInterval
}
