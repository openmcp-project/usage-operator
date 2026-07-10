package usage

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"time"

	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
)

type UsageTracker struct {
	Config          *usagev1alpha1.ResourceToTrack
	TraitsExtractor *TraitsExtractor
}

func NewUsageTracker(ctx context.Context, cfg *usagev1alpha1.ResourceToTrack) (*UsageTracker, error) {
	te, err := NewTraitsExtractor(cfg.Traits)
	if err != nil {
		return nil, fmt.Errorf("error creating traits extractor: %w", err)
	}

	return &UsageTracker{
		Config:          cfg,
		TraitsExtractor: te,
	}, nil
}

// NamespaceRequired returns true if either the selector or any trait definition requires the namespace to be fetched for the resource, false otherwise.
func (u *UsageTracker) NamespaceRequired() bool {
	if u.Config.Selector != nil && u.Config.Selector.NamespaceSelector != nil {
		return true
	}
	for _, traitDef := range u.Config.Traits {
		if strings.HasPrefix(traitDef.Path, ".namespace") {
			return true
		}
	}
	return false
}

// MatchesSelector returns true if the given object matches the configured selectors for the resource and its namespace, if any.
// Always returns true if no selectors are configured or an error occurred while fetching the namespace or evaluating the selectors.
// The namespace argument may be nil if there is no namespace selector configured.
func (u *UsageTracker) MatchesSelector(ctx context.Context, obj client.Object, ns *corev1.Namespace) (bool, error) {
	// use resource and namespace selectors to filter the events, if specified
	if u.Config.Selector != nil {
		if u.Config.Selector.ResourceSelector != nil {
			sel, err := metav1.LabelSelectorAsSelector(u.Config.Selector.ResourceSelector)
			if err != nil {
				return true, fmt.Errorf("error converting resource selector to selector: %w", err)
			}
			if !sel.Matches(labels.Set(obj.GetLabels())) {
				return false, nil
			}
		}
		if u.Config.Selector.NamespaceSelector != nil && (u.Config.Selector.NamespaceSelector.Names != nil || u.Config.Selector.NamespaceSelector.Selector != nil) {
			// dummy check: if the name selector is not nil, but an empty list, then no namespace is selected, so we can return false immediately
			if u.Config.Selector.NamespaceSelector.Names != nil && len(u.Config.Selector.NamespaceSelector.Names) == 0 {
				return false, nil
			}
			if u.Config.Selector.NamespaceSelector.Names != nil {
				if !slices.Contains(u.Config.Selector.NamespaceSelector.Names, ns.Name) {
					return false, nil
				}
			} else if u.Config.Selector.NamespaceSelector.Selector != nil {
				sel, err := metav1.LabelSelectorAsSelector(u.Config.Selector.NamespaceSelector.Selector)
				if err != nil {
					return true, fmt.Errorf("error converting namespace selector to selector: %w", err)
				}
				if !sel.Matches(labels.Set(ns.GetLabels())) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// NewResourceUsage creates a new ResourceUsage object (internally only, not on the cluster).
// This method is intended to be called if data is to be tracked, but either no ResourceUsage exists for the tracked object yet,
// or the latest ResourceUsage's tracking period has ended and a new one needs to be created.
// Note that this method does create a new ResourceUsage object on the cluster, it just prepares the resource.
// The given time is truncated to minute precision and used as the start time for the new ResourceUsage's tracking period and usage tracking.
func (u *UsageTracker) NewResourceUsage(obj client.Object, traitData map[string][]byte, now time.Time) *usagev1alpha1.ResourceUsage {
	now = now.Truncate(time.Minute)
	res := &usagev1alpha1.ResourceUsage{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s-%s-", u.Config.Kind, ctrlutils.NameHashSHAKE128Base32(u.Config.Group, u.Config.Version, u.Config.Kind, obj.GetNamespace(), obj.GetName())), ctrlutils.K8sMaxNameLength-10),
		},
		Spec: usagev1alpha1.ResourceUsageSpec{
			Resource: usagev1alpha1.ResourceReference{
				GroupVersionKind: u.Config.GroupVersionKind,
				ObjectReference: commonapi.ObjectReference{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				},
			},
			TrackingPeriod: usagev1alpha1.Timespan{
				Start: new(metav1.NewTime(now)),
				End:   new(metav1.NewTime(now.Add(u.Config.ResourceUsagePeriod.Duration).Truncate(time.Minute))),
			},
			Usage: usagev1alpha1.Timespans{
				{
					Start: new(metav1.NewTime(now)),
				},
			},
			Traits: make(map[string]usagev1alpha1.TraitUsages, len(traitData)),
		},
	}

	for name, data := range traitData {
		res.Spec.Traits[name] = usagev1alpha1.TraitUsages{
			{
				Usage: usagev1alpha1.Timespans{
					{
						Start: new(metav1.NewTime(now)),
					},
				},
				Value: runtime.RawExtension{
					Raw: data,
				},
			},
		}
	}

	return res
}

// CompleteResourceUsage is meant to be called on a ResourceUsage which is still 'Ongoing' according to its status, but its tracking period has ended.
// This basically 'cleans up' the ResourceUsage object and sets its status to 'Completed', so that a new ResourceUsage can be created for the same resource.
// If the latest usage trackings (resource one as well as traits) don't have an end time set, it will be set to the end of the tracking period.
// Modifies the object in-place (status, potentially also spec), but does not update it on the cluster. This must be done by the caller.
// This method can be called on a nil UsageTracker.
func (u *UsageTracker) CompleteResourceUsage(usage *usagev1alpha1.ResourceUsage) {
	// set end time for the latest usage tracking if not set
	if len(usage.Spec.Usage) > 0 && usage.Spec.Usage[0].End.IsZero() {
		usage.Spec.Usage[0].End = usage.Spec.TrackingPeriod.End.DeepCopy()
	}

	// set end time for trait trackings if not set
	for trait := range usage.Spec.Traits {
		for i := range usage.Spec.Traits[trait] {
			if usage.Spec.Traits[trait][i].Usage[0].End.IsZero() {
				usage.Spec.Traits[trait][i].Usage[0].End = usage.Spec.TrackingPeriod.End.DeepCopy()
			}
		}
	}

	// set status
	usage.Status.TotalTrackedDuration = new(metav1.Duration{Duration: usage.Spec.Usage.TotalDuration()})
	usage.Status.Phase = usagev1alpha1.UsagePhaseCompleted
}

// StopTracking is meant to be called on a ResourceUsage which is still 'Ongoing', but the resource is no longer tracked (e.g. because it was deleted or modified to not match a selector anymore).
// Note that this does not set the status to 'Completed', as further information might be added to the ResourceUsage if the resource is recreated or modified to match the selector again.
// Modifies the object in-place (spec only), but does not update it on the cluster. This must be done by the caller.
// This method can be called on a nil UsageTracker.
func (u *UsageTracker) StopTracking(usage *usagev1alpha1.ResourceUsage, now time.Time) {
	now = now.Truncate(time.Minute)
	// set end time for the latest usage tracking if not set
	if len(usage.Spec.Usage) > 0 && usage.Spec.Usage[0].End.IsZero() {
		usage.Spec.Usage[0].End = new(metav1.NewTime(now))
	}

	// set end time for trait trackings if not set
	for trait := range usage.Spec.Traits {
		for i := range usage.Spec.Traits[trait] {
			if usage.Spec.Traits[trait][i].Usage[0].End.IsZero() {
				usage.Spec.Traits[trait][i].Usage[0].End = new(metav1.NewTime(now))
			}
		}
	}
}

// Track tracks new usage in the given ResourceUsage object for the given resource and its traits, based on the given trait data.
// This will modify the ResourceUsage object in-place (spec only), but does not update it on the cluster. This must be done by the caller.
// This method can be called on a nil UsageTracker.
func (u *UsageTracker) Track(usage *usagev1alpha1.ResourceUsage, obj client.Object, traitData map[string][]byte, now time.Time) {
	now = now.Truncate(time.Minute)

	// check if tracking is currently active
	if len(usage.Spec.Usage) == 0 || !usage.Spec.Usage[0].End.IsZero() {
		// tracking is not active, add a new entry to the usage slice
		usage.Spec.Usage = append([]usagev1alpha1.Timespan{
			{
				Start: new(metav1.NewTime(now)),
			},
		}, usage.Spec.Usage...)
	}

	// and now, we do basically the same for all traits, except that we also need to check if the trait value has changed
	for trait, data := range traitData {
		_, exists := usage.Spec.Traits[trait]
		if !exists {
			// trait is not tracked yet, add a new entry to the traits map
			usage.Spec.Traits[trait] = usagev1alpha1.TraitUsages{
				{
					Value: runtime.RawExtension{
						Raw: data,
					},
					Usage: usagev1alpha1.Timespans{
						{
							Start: new(metav1.NewTime(now)),
						},
					},
				},
			}
			continue
		}

		// trait is tracked, check if the value has changed
		// first, we need to identify the last known value (has no end time set)
		currentIdx := -1
		sameValueIdx := -1
		for i := range usage.Spec.Traits[trait] {
			if len(usage.Spec.Traits[trait][i].Usage) > 0 && usage.Spec.Traits[trait][i].Usage[0].End.IsZero() {
				currentIdx = i
				if sameValueIdx >= 0 {
					break
				}
			}
			if bytes.Equal(usage.Spec.Traits[trait][i].Value.Raw, data) {
				sameValueIdx = i
				if currentIdx >= 0 {
					break
				}
			}
		}
		if currentIdx >= 0 && sameValueIdx >= 0 && currentIdx == sameValueIdx {
			// the value is the same as the last known value, nothing to do for this trait
			continue
		}
		if currentIdx >= 0 {
			// the trait is being tracked, but with a different value
			usage.Spec.Traits[trait][currentIdx].Usage[0].End = new(metav1.NewTime(now))
		}
		if sameValueIdx >= 0 {
			// there is already an entry for the new value
			usage.Spec.Traits[trait][sameValueIdx].Usage = append([]usagev1alpha1.Timespan{
				{
					Start: new(metav1.NewTime(now)),
				},
			}, usage.Spec.Traits[trait][sameValueIdx].Usage...)
		} else {
			// there is no entry for the new value yet, add a new one
			usage.Spec.Traits[trait] = append([]usagev1alpha1.TraitUsage{
				{
					Value: runtime.RawExtension{
						Raw: data,
					},
					Usage: usagev1alpha1.Timespans{
						{
							Start: new(metav1.NewTime(now)),
						},
					},
				},
			}, usage.Spec.Traits[trait]...)
		}
	}
	// in case a trait got removed from the configuration, we need to stop tracking it
	for trait := range usage.Spec.Traits {
		if _, exists := traitData[trait]; !exists {
			// trait is no longer tracked, stop tracking it
			for i := range usage.Spec.Traits[trait] {
				if len(usage.Spec.Traits[trait][i].Usage) > 0 && usage.Spec.Traits[trait][i].Usage[0].End.IsZero() {
					usage.Spec.Traits[trait][i].Usage[0].End = new(metav1.NewTime(now))
				}
			}
		}
	}
}
