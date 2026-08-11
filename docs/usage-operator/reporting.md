# Reporting Usage

The usage-operator is only responsible for tracking the usage of resources. Reporting it to some kind of metering service is out of scope, as it is highly dependent on the environment.

## API Utils

The `ResourceUsage` resource is designed so that its tracking period can start and end at any time, and it records modification timestamps instead of durations. While this format holds more information than if just the duration was tracked, it makes answering the question 'for how long was the resource used within a given time frame' non-trivial, as potentially multiple `ResourceUsages` and some calculations are required for this.

To not put the burden of computing the usage durations on a depending controller, the `api/utils` package contains some helper functions which are shortly explained in this section.

#### General Information

Most of the helper functions take lists of pointers to `ResourceUsage` objects (`[]*v1alpha1.ResourceUsage`) as arguments. These `ResourceUsage` objects are always expected to belong to **only a single resource**, meaning `spec.resource` should be identical for all of them. Passing in `ResourceUsage` objects belonging to different resources leads to undefined behavior.

The `Pointerize` function can help with transforming the `Items` slice of `ResourceUsageList` objects filled by the `client.Client`'s `List(...)` method into something that can be passed into the helper functions.

Start times are always considered to be inclusive, end times are exclusive. All times are truncated to minute precision.

#### ComputeUsageDuration(start, end time.Time, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, error)

`ComputeUsageDuration` gives the answer to the question 'for how long was the tracked object used within the given time frame?'.
As a second return value, it returns the part of the requested time frame which was not covered by the given `ResourceUsage` objects. If this is non-zero, the tracked resource might actually have been used for more than the returned time frame.

It does not return durations for any traits.

#### ComputeTraitUsageDuration(start, end time.Time, traitName string, traitValue any, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, error)

`ComputeTraitUsageDuration` answers the question `for how long within the given time frame did trait X have value Y?'.
It works similarly to `ComputeUsageDuration`, but takes a trait identifier and a value for comparison in addition.

If the trait's value to compare against is passed in as `[]byte`, `JSON` object from the `k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1` package, or pointer to the latter one, the raw byte slice will be used. Otherwise, the given object will be marshaled into JSON to create a byte slice. `bytes.Equal` will then be used to compare the value from the arguments with the recorded trait values from the `ResourceUsage` resources, with minor custom logic ensuring that an empty byte slice and one containing only `null` are considered equal.

#### ComputeUsageDurationWithTraits(start, end time.Time, usages ...*v1alpha1.ResourceUsage) (time.Duration, time.Duration, map[string]TraitValueDurations, error)

`ComputeUsageDurationWithTraits` can be seen as a combination of `ComputeUsageDuration` and `ComputeTraitUsageDuration`. It returns the total usage duration within the specified time frame and the remainder duration for which no tracking information was given, and it returns a mapping that contains the information which value each trait had for how long.

`TraitValueDurations` is a list of (pointers to) `TraitValueDuration` struct, which just pair the trait's value together with the duration the trait had this value for. As a convenience method, `GetDurationForValue(value any)` can be called on `TraitValueDurations` to return the duration for a specific value. The value will be marshaled into JSON if it is not already raw JSON, as explained for `ComputeTraitUsageDuration`.
