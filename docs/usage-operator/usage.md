# The ResourceUsage Resource

When the usage-operator tracks a resource's usage, it stores the information into `ResourceUsage` resources, which it puts on the same cluster the tracked resources are on.

## ResourceUsage

A `ResourceUsage` resource is in one of two states: it is either *ongoing* or *completed*. This is also denoted by the resource's `status.phase` field. The main difference is that new information will be added to ongoing `ResourceUsage`s, while the completed ones should not change anymore.

Independent of its phase, the following should be true for all `ResourceUsage` resources:
- `spec.trackingPeriod` has `start` and `end` set.
- Each `ResourceUsage` belongs to a specific resource, as denoted by its `spec.resource`. For each tracked resource, there can be any amount of _completed_ `ResourceUsage` objects - they will pile up if not garbage collected in some way - but there should never be more than one _ongoing_ one.
  - If a tracked resource is deleted and then re-created (with the same name) within its ongoing `ResourceUsage`'s tracking period, tracking will be continued in this `ResourceUsage`. A new `ResourceUsage` is only created when the previous one's tracking duration has ended (and the resource still exists and is being tracked).
- The tracking periods (`spec.trackingPeriods`) for all `ResourceUsage` objects belonging to the same tracked resources should never overlap.
  - Note that the tracking period's start time is inclusive, while its end time is exclusive, so having a `ResourceUsage` with the same start time as another one's end time is fine.
- All times in the `ResourceUsage` resource are truncated to minute precision.
- `ResourceUsage`s are created with a generated name that consists of the tracked resource's _kind_ (lowercase), a hash value of the resource's identity (GVK + namespace + name), and a random suffix, all separated by dashes.
  - This means that all `ResourceUsage` objects belonging to e.g. secrets will start with `secret-`, and all `ResourceUsage`s belonging to the same secret will have the same hash following the prefix.
- All list of usage timespans - `spec.usage` and `spec.traits.<trait>[*].usage` - are sorted from new to old, meaning the most recent - possibly still ongoing - timespan is the first entry of the list.
  - Also, each timespan is expected to have a start time set. The end time may be nil, see below for details.
  - Timespans from the same list should never overlap and all start and end times should be within the tracking period specified in `spec.trackingPeriod`.
    - Keep in mind that the end time is exclusive.


### Ongoing

An ongoing `ResourceUsage` resource looks like this:
```yaml
apiVersion: usage.open-control-plane.io/v1alpha1
kind: ResourceUsage
metadata:
  generateName: controlplane-md5fsmsf-
  name: controlplane-md5fsmsf-8x9wx
spec:
  resource:
    group: core.open-control-plane.io
    kind: ControlPlane
    name: test
    namespace: project-d073016--ws-test
    version: v2alpha1
  trackingPeriod:
    end: "2026-07-18T20:30:00Z"
    start: "2026-07-15T20:30:00Z"
  traits:
    flavor:
    - usage:
      - start: "2026-07-16T15:15:00Z"
      value: standard
    - usage:
      - end: "2026-07-16T15:15:00Z"
        start: "2026-07-15T20:30:00Z"
      value: premium
    project:
    - usage:
      - start: "2026-07-15T20:30:00Z"
      value: my-project
    workspace:
    - usage:
      - start: "2026-07-15T20:30:00Z"
      value: my-workspace
  usage:
  - start: "2026-07-15T20:30:00Z"
status:
  phase: Ongoing
```

For _ongoing_ `ResourceUsage` objects, the following properties can be expected:
- `status.phase` is `Ongoing`.
- Usually, `spec.trackingPeriod.end` is in the future.
  - The usage-operator tries to complete ongoing `ResourceUsage` objects shortly after their tracking period has ended. If it is not running or has problems with reconciliation, ongoing `ResourceUsage` objects with a tracking period that has already ended can exist. They should be completed when the usage-operator successfully reconciled the tracked resource.
- `spec.usage[0]` can have no end time.
  - This indicates that the tracked resource currently exists and is still being tracked.
- Each trait (under `spec.traits.<trait>`) can have one entry where `usage[0]` has no end time.
  - This indicates a trait's current value.

### Completed

This would be an example for a completed `ResourceUsage` object:
```yaml
apiVersion: usage.open-control-plane.io/v1alpha1
kind: ResourceUsage
metadata:
  generateName: controlplane-md5fsmsf-
  name: controlplane-md5fsmsf-8x9wx
spec:
  resource:
    group: core.open-control-plane.io
    kind: ControlPlane
    name: test
    namespace: project-d073016--ws-test
    version: v2alpha1
  trackingPeriod:
    end: "2026-07-18T20:30:00Z"
    start: "2026-07-15T20:30:00Z"
  traits:
    flavor:
    - usage:
      - end: 2026-07-17T20:30:00Z
        start: "2026-07-16T15:15:00Z"
      value: standard
    - usage:
      - end: "2026-07-16T15:15:00Z"
        start: "2026-07-15T20:30:00Z"
      value: premium
    project:
    - usage:
      - end: 2026-07-17T20:30:00Z
        start: "2026-07-15T20:30:00Z"
      value: my-project
    workspace:
    - usage:
      - end: 2026-07-17T20:30:00Z
        start: "2026-07-15T20:30:00Z"
      value: my-workspace
  usage:
  - end: 2026-07-17T20:30:00Z
    start: "2026-07-15T20:30:00Z"
status:
  phase: Completed
  totalTrackedDuration: 48h0m0s
```

_Completed_ `ResourceUsage` resources have the following properties:
- `status.phase` is `Completed`.
- `status.totalTrackedDuration` is set.
  - This holds the total time for which the resource was used during the tracking period - assuming the configuration was not changed to not track the resource anymore, this equals the duration for which the resource existed within the tracking period.
  - This value should never be greater than the duration of the tracking period.
- All entries in all lists of usage timespans have an end time set.
  - For traits, this end time means that one of the following happened at this time:
    - the trait's value changed
    - the config was changed to not track this trait anymore
    - the config was changed to not track this resource anymore
    - the resource was deleted
    - the tracking period ended
  - For entries in `spec.usage`, any of these events can cause the end time to be set:
    - the config was changed to not track this resource anymore
    - the resource was deleted
    - the tracking period ended
- Completed `ResourceUsage`s are 'final' and should not be modified anymore.
  - This is not enforced in any way.

## Missed Reconciliations

The usage-operator needs to reconcile tracked resources when they are modified, in order to check if something relevant changed, so it can then update the corresponding `ResourceUsage` object accordingly. It also needs to reconcile when the tracking period of the currently ongoing `ResourceUsage` object has ended, so it can complete it and create a new one. The problem is that it can always happen that any of these reconciliations does not happen for some reason, the most prominent one being that the usage-operator is not running in that moment.

Since the usage-operator never knows during a reconciliation why it was triggered and whether some reconciliation triggers were missed since the last one, it has to take some assumptions, which are explained in this section.

> [!TIP]
> TL;DR: Tracking is less precise if tracked properties (resource existence / trait values) change rapidly. Or if the usage-operator is not working continuously. 

#### Startup Reconciliations

Not really an assumption, but when the usage-operator is started, it enqueues reconciliations for all resources for which _ongoing_ `ResourceUsage`s exist. This includes resources which are not tracked anymore according to the config. The sole reason for this logic is to ensure that all ongoing `ResourceUsage`s are eventually completed and none are left _ongoing_, even if their resources have been removed from the resources to track in the config.

#### Reconcile - Ongoing ResourceUsage

If a tracked resource is reconciled and an ongoing `ResourceUsage` exists for it, the usage-operator assumes that the resource did not change since the last reconciliation. This means that if since the last reconciliation no trait changed, the resource did not change its state of existence, and the `ResourceUsage`'s tracking period has not ended, the reconciliation does not modify the `ResourceUsage`. If any trait's value changed or the resource got deleted or re-created, the corresponding timestamps in the `ResourceUsage` will be set to the time of reconciliation.

Note that if a resource is deleted _and_ re-created in between two reconciliations, the time in which it did not exist is not tracked. According to the `ResourceUsage`, the resource will have existed for the whole time. A similar effect occurs when a resource is created and deleted before being reconciled, or a resource's trait's value is changed to something else and then back again in between two reconciliations.

#### Reconcile - New ResourceUsage

If a resource is reconciled and a new `ResourceUsage` needs to be created - either because there was no previous one or because it's tracking period has ended - the start of the new `ResourceUsage`'s tracking period is dated back to either the previous `ResourceUsage`'s end time or the resource's creation timestamp, whatever happened last. This is meant to avoid gaps in tracking if a resource's reconciliation is delayed, but it assumes that the resource did not change since the computed start time. While it is not possible to falsely track the resource's existence this way (due to the creation timestamp being take into account), changes to traits might get lost this way.

## Tips

#### Aliases

`usage` and `ru` can be used as aliases for `resourceusage` in calls to `kubectl get`.

#### Printer Columns

By default, `kubectl get` shows the following columns when listing `ResourceUsage` objects:
```
NAME                          KIND           NAME   NAMESPACE                    START   END         PHASE
controlplane-md5fsmsf-42wqc   ControlPlane   test   project-myproject--ws-myws   40h     <invalid>   Ongoing
```

By specifying `-o wide`, further columns can be shown:
```
NAME                          KIND           VERSION    GROUP                        NAME   NAMESPACE                    START   END         PHASE
controlplane-md5fsmsf-42wqc   ControlPlane   v2alpha1   core.open-control-plane.io   test   project-myproject--ws-myws   40h     <invalid>   Ongoing
```

> [!NOTE]
> The end time is shown as `invalid` because it lies in the future.

#### Resource Filtering

For `ResourceUsage`s, `status.phase` as well as all fields of `spec.resource` are added to the cache's index. This allowes filtering for these fields.

Example `kubectl`:
```shell
kubectl get resourceusage --field-selector spec.resource.kind=ControlPlane
```

Example code snippet:
```golang
rul := &usagev1alpha1.ResourceUsageList{}
myclient.List(ctx, rul, client.MatchingFields{
  "spec.resource.group":     req.Group,
  "spec.resource.version":   req.Version,
  "spec.resource.kind":      req.Kind,
  "spec.resource.name":      req.Name,
  "spec.resource.namespace": req.Namespace,
})
```
