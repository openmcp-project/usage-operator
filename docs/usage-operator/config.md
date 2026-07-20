# Configuration

The usage-operator requires a configuration that tells it which resources to track usage for. It comes in form of a custom resource named `UsageServiceConfig`, which must have the same name as the controller's `PlatformService` (which is passed in via the `--provider-name` argument).

If the configuration resource is missing, usage will not be tracked for any resource.

```yaml
apiVersion: usage.open-control-plane.io/v1alpha1
kind: UsageServiceConfig
metadata:
  name: usage
spec:
  garbageCollection:     # optional
    interval: 1h         # optional, defaults to 24h
    keepDuration: 24h    # optional
    keepCount: 2         # optional
    andConditions: false # optional, defaults to false
  resourcesToTrack:
  - group: core.open-control-plane.io
    version: v2alpha1
    kind: ControlPlane
    selector: # optional, see below for selector specification
      namespace:
        selector:
          matchExpressions:
          - key: core.openmcp.cloud/project
            operator: Exists
          - key: core.openmcp.cloud/workspace
            operator: Exists
    resourceUsagePeriod: 72h # optional, defaults to 720h (30 days)
    trackUntil: DeletionTimestamp # optional, defaults to 'Deletion'
    traits: # optional
      project: # arbitrary identifier
        path: .namespace.metadata.labels.core\.openmcp\.cloud/project
      workspace: # arbitrary identifier
        path: .namespace.metadata.labels.core\.openmcp\.cloud/workspace
      flavor: # arbitrary identifier
        path: .resource.metadata.labels.core\.openmcp\.cloud/flavor
  - group: ""
    version: v1
    kind: Secret
```

### Garbage Collection

The usage-operator creates `ResourceUsage` resources. To avoid them from piling up, they need to be deleted at some point. This can be done from the outside, but it is also possible to configure garbage collection to have the usage-operator clean them up itself.

Garbage collection is configured via `spec.garbageCollection` in the config. If the field does not exist, garbage collection is disabled.

Fields of `spec.garbageCollection`:
- **`interval` _(optional, default `24h`)_** - Specifies the interval at which the garbage collection runs.
- **`keepDuration` _(optional)_** - If non-empty, completed `ResourceUsage` resources will be deleted if their tracking duration's end time is older than specified here.
- **`keepCount` _(optional)_** - If greater than 0, only this many completed `ResourceUsage` objects will be kept _per tracked resource_.
- **`andConditions` _(optional, default `false`)_** - Specifies the behavior if `keepDuration` and `keepCount` are specified. By default, they are ORed, so completed `ResourceUsage` objects will be deleted if they are either older than the specified keep duration or exceed the keep count. If this is true, the conditions are ANDed instead, so `ResourceUsage`s will only be deleted if both are true. This configuration only has an effect if `keepDuration` and `keepCount` are both set.

> [!IMPORTANT]
> In order to actually have resources deleted, at least one of `keepDuration` and `keepCount` needs to be specified - if both are at their default values, no garbage collection will happen.

### Resources to Track

`spec.resourcesToTrack` specifies which resources' usage should be tracked by the usage-operator. Multiple resource kinds can be tracked by adding corresponding entries to this list.

#### Resource Specification

`group`, `version`, and `kind` are the only required fields of an `resourcesToTrack` entry and specify the resource kind which should be tracked.

> [!WARNING]
> Each GVK must be unique among all entries of `resourcesToTrack`. Multiple entries for the same GVK are not allowed.

#### Tracking Behavior

There are some ways to influence the tracking behavior:
- **`resourceUsagePeriod` _(optional, default `720h`)_** - Specifies the period that is contained in a single `ResourceUsage` object. Smaller values will lead to more `ResourceUsage` resources, while larger values can lead to bigger ones.
  - Changes to this value affect only newly created `ResourceUsage` resources, not already existing ones.
- **`trackUntil` _(optional, default `Deletion`)_** - Defines when a resource's usage is considered to have ended. Valid values are `Deletion` and `DeletionTimestamp`. The default is the former one, meaning a resource's usage ends when the resource ceases to exist. If set to `DeletionTimestamp`, usage will already end as soon as the tracked resource gets a deletion timestamp - the timespan between getting the deletion timestamp and its actual deletion is not tracked as usage anymore in this case.

#### Selectors

If not all resources of the specified group/version/kind should be tracked, the optional `selector` field can be used to filter the tracked resources:
- **`resource` _(optional)_** - Holds a standard k8s label selector (`matchLabels` and/or `matchExpressions` can be specified) which is applied to the resource itself.
- **`namespace` _(optional)_** - Can be used to restrict usage tracking for the given resource kind to specific namespaces.
  - **`names` _(optional)_** - A string array of namespace names. If specified, only resources within these namespaces will be tracked.
    - Note that there is a difference between a nil value (`names` not specified or set to `null`) and an empty array (`[]`) - the former one disables filtering for namespace identities, while the latter one results in a selector which will not select anything, effectively disabling resource tracking for this entry.
  - **`selector` _(optional)_** - This is again a standard k8s label selector, but it will be used to filter the resource's namespace, not the resource itself.

If resource and namespace selectors are specified, they will be ANDed.
If a namespace selector contains `names` and `selector`, the former one overrides the latter one, so only the namespace's name will determine whether the resource is selected or not, the namespace label selector will be ignored.

#### Traits

For some scenarios, not only the time for which a specific resource existed, but also parts of its configuration might be interesting. For example, if a resource can use a 'standard' or a 'premium' class. These cases are covered with the concept of 'traits'. 

Each trait consists of a unique identifier - the map key under `traits` in the configuration - and a path pointing to a specific location within the tracked resource's manifest.
The path is specified in standard [JSONPath](https://goessner.net/articles/JsonPath/) syntax, but it can refer to either the resource itself or to its namespace, so it needs to be prefixed with `.resource` or `.namespace`, respectively. 

> [!CAUTION]
> Using a trait path prefixed with `.namespace` on a cluster-scoped resource will cause errors during runtime.

All trait's paths are evaluated whenever the tracked resource is modified. For resources with traits accessing their namespaces, this will also happen when the namespace is modified. Whatever the path expression resolves to is considered that trait's _value_. Paths which cannot be resolved result in a value of `null`.

The `ResourceUsage` object for a specific resource will not only track the resource's existence, but also the values of all of the traits defined for its GVK.

> [!IMPORTANT]
> It is strongly recommended to limit trait definitions to parts of the resource's manifest that don't change often. If a trait's value changes into something new often, this can lead to the corresponding `ResourceUsage` object growing large quickly, which also puts additional load on the usage-operator.
