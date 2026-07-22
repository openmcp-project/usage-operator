package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/openmcp-project/usage-operator/api/v1alpha1"
)

// Pointerize takes a slice of objects and returns a slice of pointers to those objects.
// It can be used to make the <object>List.Items slice from client.List calls compatible with the functions below.
func Pointerize[T any](objects []T) []*T {
	res := make([]*T, len(objects))
	for i := range objects {
		res[i] = &objects[i]
	}
	return res
}

// ComputeUsageDuration takes a start and end time, and any amount of ResourceUsage objects.
// It computes the total duration within start (inclusive) and end (exclusive) for which the resource was tracked.
// The second return value contains the remainder duration for which no tracking information exists within the given ResourceUsage objects.
// Returns an error, if the end time is earlier than the start time. Also returns an error, if any of the given ResourceUsage objects does not have a valid tracking period (start and end time must be set and the start time must be earlier than the end time).
// A few points should be noted for this function to behave as expected:
//   - All given ResourceUsage objects are taken into account. This means that only ResourceUsage objects belonging to the same resource (GVK + namespace + name) should be passed to this function,
//     and also only completed ones, unless data from ongoing ResourceUsage objects is explicitly desired.
//   - There should never exist any future ResourceUsage objects, which means that the end time should never be later than 'now'.
//   - All returned durations are truncated to the nearest minute, any seconds or smaller units are discarded.
//   - The sum of the first and second return values will never be greater than the duration between start and end.
func ComputeUsageDuration(start, end time.Time, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, error) {
	start = start.Truncate(time.Minute)
	end = end.Truncate(time.Minute)
	if end.Before(start) {
		return 0, 0, fmt.Errorf("end time %s is before start time %s", end, start)
	}
	window := end.Sub(start)
	if window == 0 {
		return 0, 0, nil
	}

	if err := validateTrackingPeriods(usages...); err != nil {
		return 0, 0, err
	}

	var intervals []interval
	coveredByTracking := time.Duration(0)

	for _, u := range usages {
		tpStart, tpEnd := trackingPeriodBounds(u)
		clippedTPStart := maxTime(tpStart, start)
		clippedTPEnd := minTime(tpEnd, end)
		if !clippedTPStart.Before(clippedTPEnd) {
			continue
		}
		coveredByTracking += clippedTPEnd.Sub(clippedTPStart)

		for _, ts := range u.Spec.Usage {
			if ts.Start.IsZero() || ts.End.IsZero() {
				continue
			}
			s := maxTime(ts.Start.Time, start)
			e := minTime(ts.End.Time, end)
			if s.Before(e) {
				intervals = append(intervals, interval{s, e})
			}
		}
	}

	used := mergeAndSum(intervals).Truncate(time.Minute)
	remainder := max((window - coveredByTracking.Truncate(time.Minute)).Truncate(time.Minute), 0)
	return used, remainder, nil
}

// ComputeTraitUsageDuration works similarly to ComputeUsageDuration, but instead of the resource's usage, it computes the total duration for which a specific trait had a specific value within the given time frame.
// The second return value contains the remainder duration for which no tracking information exists within the given ResourceUsage objects.
// It will include the duration for which the ResourceUsage objects exist, but do not contain any information about the given trait (usually happens if the configuration was changed to contain the trait at some point and the data comes from before and after that change).
// The trait's value can be passed in either as raw JSON ([]byte type) or as a Go value (any type). In the latter case, the value will be marshaled to JSON before comparison.
// The same points mentioned for ComputeUsageDuration also apply here.
func ComputeTraitUsageDuration(start, end time.Time, traitName string, traitValue any, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, error) {
	wantJSON, err := toJSON(traitValue)
	if err != nil {
		return 0, 0, fmt.Errorf("marshaling trait value: %w", err)
	}

	start = start.Truncate(time.Minute)
	end = end.Truncate(time.Minute)
	if end.Before(start) {
		return 0, 0, fmt.Errorf("end time %s is before start time %s", end, start)
	}
	window := end.Sub(start)
	if window == 0 {
		return 0, 0, nil
	}

	if err := validateTrackingPeriods(usages...); err != nil {
		return 0, 0, err
	}

	var matchIntervals []interval
	coveredByTracking := time.Duration(0)

	for _, u := range usages {
		tpStart, tpEnd := trackingPeriodBounds(u)
		clippedTPStart := maxTime(tpStart, start)
		clippedTPEnd := minTime(tpEnd, end)
		if !clippedTPStart.Before(clippedTPEnd) {
			continue
		}
		coveredByTracking += clippedTPEnd.Sub(clippedTPStart)

		traitUsages, hasTrait := u.Spec.Traits[traitName]
		if !hasTrait {
			continue
		}
		for _, tu := range traitUsages {
			if !RawJSONValueEqual(tu.Value.Raw, wantJSON) {
				continue
			}
			for _, ts := range tu.Usage {
				if ts.Start.IsZero() || ts.End.IsZero() {
					continue
				}
				s := maxTime(ts.Start.Time, start)
				e := minTime(ts.End.Time, end)
				if s.Before(e) {
					matchIntervals = append(matchIntervals, interval{s, e})
				}
			}
		}
	}

	matched := mergeAndSum(matchIntervals).Truncate(time.Minute)
	remainder := max((window - coveredByTracking.Truncate(time.Minute)).Truncate(time.Minute), 0)
	return matched, remainder, nil
}

// ComputeUsageDurationWithTraits works similarly to ComputeUsageDuration, but in addition to the total usage duration and the remainder duration, it also computes the total duration for which each trait had each value within the given time frame.
// The same points mentioned for ComputeUsageDuration also apply here.
func ComputeUsageDurationWithTraits(start, end time.Time, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, map[string]TraitValueDurations, error) {
	used, remainder, err := ComputeUsageDuration(start, end, usages...)
	if err != nil {
		return 0, 0, nil, err
	}

	start = start.Truncate(time.Minute)
	end = end.Truncate(time.Minute)

	// traitName -> raw JSON key -> intervals
	traitIntervals := map[string]map[string][]interval{}

	for _, u := range usages {
		tpStart, tpEnd := trackingPeriodBounds(u)
		clippedTPStart := maxTime(tpStart, start)
		clippedTPEnd := minTime(tpEnd, end)
		if !clippedTPStart.Before(clippedTPEnd) {
			continue
		}

		for traitName, traitUsages := range u.Spec.Traits {
			if _, ok := traitIntervals[traitName]; !ok {
				traitIntervals[traitName] = map[string][]interval{}
			}
			for _, tu := range traitUsages {
				key := string(tu.Value.Raw)
				for _, ts := range tu.Usage {
					if ts.Start.IsZero() || ts.End.IsZero() {
						continue
					}
					s := maxTime(ts.Start.Time, start)
					e := minTime(ts.End.Time, end)
					if s.Before(e) {
						traitIntervals[traitName][key] = append(traitIntervals[traitName][key], interval{s, e})
					}
				}
			}
		}
	}

	result := map[string]TraitValueDurations{}
	for traitName, valueMap := range traitIntervals {
		for rawJSON, ivs := range valueMap {
			d := mergeAndSum(ivs).Truncate(time.Minute)
			result[traitName] = append(result[traitName], &TraitValueDuration{
				Value:    apiextensionsv1.JSON{Raw: []byte(rawJSON)},
				Duration: d,
			})
		}
	}

	return used, remainder, result, nil
}

type TraitValueDuration struct {
	// Value is the value of the trait.
	// +nullable
	Value apiextensionsv1.JSON `json:"value"`
	// Duration is the duration for which the trait had this value.
	Duration time.Duration `json:"duration"`
}

type TraitValueDurations []*TraitValueDuration

// GetDurationForValue returns the duration for which the trait had the given value.
// The value can be passed in either as raw JSON ([]byte type) or as a Go value (any type). In the latter case, the value will be marshaled to JSON before comparison.
// If the value is not found in the list, a duration of 0 is returned (no error).
func (tvds TraitValueDurations) GetDurationForValue(value any) (time.Duration, error) {
	wantJSON, err := toJSON(value)
	if err != nil {
		return 0, fmt.Errorf("marshaling value: %w", err)
	}
	for _, tvd := range tvds {
		if RawJSONValueEqual(tvd.Value.Raw, wantJSON) {
			return tvd.Duration, nil
		}
	}
	return 0, nil
}

// GetDominantTraitValue returns the *TraitValueDuration with the longest duration.
// In case of a tie, the first one encountered is returned.
// If includeNull is false, any TraitValueDuration with a value of 'null' will be ignored.
// If there are no TraitValueDurations or all of them are ignored, nil is returned (no error).
func (tvds TraitValueDurations) GetDominantTraitValue(includeNull bool) *TraitValueDuration {
	var dominant *TraitValueDuration
	for _, tvd := range tvds {
		if !includeNull && RawJSONValueEqual(tvd.Value.Raw, nil) {
			continue
		}
		if dominant == nil || tvd.Duration > dominant.Duration {
			dominant = tvd
		}
	}
	return dominant
}

// validateTrackingPeriods returns an error if any ResourceUsage has an invalid tracking period.
func validateTrackingPeriods(usages ...*v1alpha1.ResourceUsage) error {
	for _, u := range usages {
		tp := u.Spec.TrackingPeriod
		if tp.Start.IsZero() || tp.End.IsZero() {
			return fmt.Errorf("ResourceUsage %q has an incomplete tracking period (start and end must both be set)", u.Name)
		}
		if !tp.Start.Time.Before(tp.End.Time) {
			return fmt.Errorf("ResourceUsage %q has an invalid tracking period: start %s is not before end %s", u.Name, tp.Start.Time, tp.End.Time)
		}
	}
	return nil
}

// trackingPeriodBounds returns the effective [start, end) of a ResourceUsage's tracking period.
// Falls back to the union of usage interval bounds if the tracking period fields are unset.
func trackingPeriodBounds(u *v1alpha1.ResourceUsage) (time.Time, time.Time) {
	var tpStart, tpEnd time.Time
	if !u.Spec.TrackingPeriod.Start.IsZero() {
		tpStart = u.Spec.TrackingPeriod.Start.Time
	}
	if !u.Spec.TrackingPeriod.End.IsZero() {
		tpEnd = u.Spec.TrackingPeriod.End.Time
	}
	if tpStart.IsZero() || tpEnd.IsZero() {
		for _, ts := range u.Spec.Usage {
			if !ts.Start.IsZero() {
				if tpStart.IsZero() || ts.Start.Time.Before(tpStart) {
					tpStart = ts.Start.Time
				}
			}
			if !ts.End.IsZero() {
				if tpEnd.IsZero() || ts.End.After(tpEnd) {
					tpEnd = ts.End.Time
				}
			}
		}
	}
	return tpStart, tpEnd
}

type interval struct {
	start time.Time
	end   time.Time
}

// mergeAndSum merges overlapping intervals (sorted by start) and returns their total duration.
func mergeAndSum(intervals []interval) time.Duration {
	if len(intervals) == 0 {
		return 0
	}
	// Insertion sort — intervals are often nearly sorted.
	for i := 1; i < len(intervals); i++ {
		for j := i; j > 0 && intervals[j].start.Before(intervals[j-1].start); j-- {
			intervals[j], intervals[j-1] = intervals[j-1], intervals[j]
		}
	}
	var total time.Duration
	cur := intervals[0]
	for _, iv := range intervals[1:] {
		if !iv.start.After(cur.end) {
			if iv.end.After(cur.end) {
				cur.end = iv.end
			}
		} else {
			total += cur.end.Sub(cur.start)
			cur = iv
		}
	}
	total += cur.end.Sub(cur.start)
	return total
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// toJSON converts a value to its JSON representation.
// []byte inputs are returned unmodified, for apiextensionsv1.JSON, their raw byte value is returned.
func toJSON(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case *apiextensionsv1.JSON:
		return val.Raw, nil
	case apiextensionsv1.JSON:
		return val.Raw, nil
	default:
		return json.Marshal(v)
	}
}

// RawJSONValueEqual compares two raw JSON values and returns true if they are equal, false otherwise.
// This is basically bytes.Equal, with the exception that an empty/nil value is considered equal to one containing only 'null'.
func RawJSONValueEqual(a, b []byte) bool {
	if len(a) == 0 {
		if len(b) == 0 || bytes.Equal(b, []byte("null")) {
			return true
		}
		return false
	}
	if len(b) == 0 {
		return bytes.Equal(a, []byte("null"))
	}
	return bytes.Equal(a, b)
}
