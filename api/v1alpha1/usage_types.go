package v1alpha1

import (
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	UsagePhaseOngoing   = "Ongoing"
	UsagePhaseCompleted = "Completed"
)

type ResourceUsageSpec struct {
	// Resource specifies the resource for which usage is being tracked.
	Resource ResourceReference `json:"resource"`

	// TrackingPeriod specifies the time frame for which this usage record is tracking usage.
	// The start time is inclusive, while the end time is exclusive.
	// The duration should be a multiple of 24 hours.
	// +optional
	TrackingPeriod Timespan `json:"trackingPeriod"`

	// Usage is a list of time periods for which the referenced resource was actually tracked.
	// In most cases, this will just be a single entry spanning the whole TrackingPeriod, but if the resource
	// is deleted (and potentially recreated) or modified to (not) match a selector during the tracking period, there may be multiple entries.
	// This can be expected to be sorted so that the latest - possibly ongoing - tracking period is first.
	// +optional
	Usage Timespans `json:"usage,omitempty"`

	// Traits maps each trait identifier to all values the trait's field has had during the tracking period, along with the duration for which each value was present.
	// +optional
	Traits map[string]TraitUsages `json:"traits,omitempty"`
}

type Timespans []Timespan

type Timespan struct {
	// Start specifies the beginning of the time period.
	Start *metav1.Time `json:"start,omitempty"`
	// End specifies the end of the time period.
	End *metav1.Time `json:"end,omitempty"`
}

// TotalDuration computes the total duration of all timespans in the slice, ignoring any timespans with a zero start or end time.
// The duration is truncated to the nearest minute, any seconds or smaller units are discarded.
func (t Timespans) TotalDuration() time.Duration {
	var total time.Duration = 0
	for _, ts := range t {
		if !ts.Start.IsZero() && !ts.End.IsZero() {
			total += ts.End.Sub(ts.Start.Time)
		}
	}

	return total.Truncate(time.Minute)
}

type ResourceReference struct {
	metav1.GroupVersionKind   `json:",inline"`
	commonapi.ObjectReference `json:",inline"`
}

type ResourceUsageStatus struct {
	// Phase specifies whether this usage record is still being tracked or if it has been completed.
	// Must be either 'Ongoing' or 'Completed'.
	// +kubebuilder:validation:Enum=Ongoing;Completed
	Phase string `json:"phase,omitempty"`

	// TotalTrackedDuration specifies the total duration for which the resource was actually tracked during the tracking period.
	// This is the sum of the durations of all entries in Usage.
	// +optional
	TotalTrackedDuration *metav1.Duration `json:"totalTrackedDuration,omitempty"`
}

type DailyUsageReport struct {
	Date    metav1.Time `json:"date"`
	Status  string      `json:"status,omitempty"`
	Message string      `json:"message,omitempty"`
}

type TraitUsages []TraitUsage

type TraitUsage struct {
	// Value is the value of the trait at the time of usage.
	// +nullable
	// +optional
	Value apiextensionsv1.JSON `json:"value"`
	// Usage specifies the time periods for which the trait had this value.
	// This can be expected to be sorted so that the latest - possibly ongoing - tracking period is first.
	// +optional
	Usage Timespans `json:"usage,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=usage;ru
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="kind",type=string,JSONPath=`.spec.resource.kind`
// +kubebuilder:printcolumn:name="version",type=string,JSONPath=`.spec.resource.version`,priority=10
// +kubebuilder:printcolumn:name="group",type=string,JSONPath=`.spec.resource.group`,priority=10
// +kubebuilder:printcolumn:name="name",type=string,JSONPath=`.spec.resource.name`
// +kubebuilder:printcolumn:name="namespace",type=string,JSONPath=`.spec.resource.namespace`
// +kubebuilder:printcolumn:name="start",type=date,JSONPath=`.spec.trackingPeriod.start`
// +kubebuilder:printcolumn:name="end",type=date,JSONPath=`.spec.trackingPeriod.end`
// +kubebuilder:printcolumn:name="phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:selectablefield:JSONPath=".spec.resource.kind"
// +kubebuilder:selectablefield:JSONPath=".spec.resource.version"
// +kubebuilder:selectablefield:JSONPath=".spec.resource.group"
// +kubebuilder:selectablefield:JSONPath=".spec.resource.name"
// +kubebuilder:selectablefield:JSONPath=".spec.resource.namespace"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
type ResourceUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceUsageSpec   `json:"spec,omitempty"`
	Status ResourceUsageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ResourceUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceUsage `json:"items"`
}

func init() {
	RegisterToSchemeBuilder(&ResourceUsage{}, &ResourceUsageList{})
}
