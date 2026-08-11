# Usage-Operator Internals

> [!NOTE]
> This document is relevant for developers only.

The usage-operator needs to dynamically start and stop tracking arbitrary resources during runtime, which comes with some complexity code-wise. This document aims to help developers to understand the code a little bit better by shortly explaining the core components of the usage-operator.

## Introduction

These are the core building blocks of the usage-operator, each of which will be explained in more detail in its own section below:

- config controller
  - Watches the platform cluster.
  - Reconciles the usage-operator's config (`UsageServiceConfig` resource), so that config changes are picked up immediately and not only after a restart.
- tracked resource controller
  - Watches the onboarding cluster.
  - This is the heart piece of the usage-operator. It reconciles arbitrary resources and tracks their existence and, if configured, traits.
  - Creates `ResourceUsage` resources to store the tracked information.
- namespace controller
  - Watches the onboarding cluster.
  - This controller reconciles namespaces and triggers reconciliations for all tracked resources within (on the tracked resource controller) if the namespace was modified.
- garbage collector
  - Periodically cleans up old `ResourceUsage` resources.
- shared information
  - A helper struct for communication between the different components.

## Config Controller

[[Code]](../../internal/controller/config.go)

The config controller reconciles `UsageServiceConfig` resources on the platform cluster. Similar to other platform service config controllers, it only reacts on changes if the modified resource's name matches the provider name (name of the `PlatformService` resource, passed in via the `--provider-name` argument) and ignores all other `UsageServiceConfig`s.

It reads the config resource and then updates the garbage collection config and the resource watches in the shared information struct. New watches are started by adding them to a map in the shared information, and if this is the first time they are added to this map (since the controller started), the `Watch` method of the controller-runtime's `controller.TypedController` interface will be called to start a corresponding informer on the onboarding cluster's cache. Since the controller-runtime does not allow to remove an informer created this way again, un-registering watches simply happens by removing the resource GVKs from the internal map again - this way, the tracked resource controller will still be triggered for reconciliation, but it will know that the resource is currently not being tracked.

Not being able to stop watches sounds somewhat problematic, but since the config is expected to only change very rarely, this should not be an issue.

## Tracked Resource Controller

[[Code]](../../internal/controller/resource.go)

The tracked resource controller is responsible for the actual tracking of resources on the onboarding cluster. Instead of implementing `reconcile.Reconciler`, it implements `reconcile.TypedReconciler`. This allows us to use the custom `TypedRequest` struct, which combines the usual name and namespace with group, version, and kind information, for reconciliation requests, thereby enabling a single controller to reconcile different resource kinds. Only one instance of this controller is started, but it will reconcile all resources that are being tracked.

The controller is started without any watches, but new ones can be added dynamically via the shared information helper. The config controller uses this to make this controller watch the correct resources whenever the config is updated/read.

Reconciliations can also be triggered manually via the shared information helper.

When a resource is reconciled, the controller fetches the latest corresponding `ResourceUsage` object (if any), and updates the tracking information (potentially creating a new `ResourceUsage` object). Note that also non-existing resources are 'reconciled' because they might have ongoing `ResourceUsage` objects which need to be completed at some point.

If a resource has an ongoing `ResourceUsage` object, then the usage-operator is stopped, the resource's GVK is removed from the config, and then the usage-operator is started again, this resource would never get reconciled again (if the config is not changed again), leading to a `ResourceUsage` object stuck in `Ongoing` phase. This could be solved by having a controller which reconciles `ResourceUsage` objects, but since we are not really interested in changes to them, we used a different approach here: During startup, all resources which have ongoing `ResourceUsage` objects will be queued for reconciliation. The controller requeues objects with ongoing `ResourceUsage` objects to reconcile again when the tracking period has just ended, so we can ensure that all ongoing `ResourceUsage`s are queued to be completed. This logic is implemented in the controller's `StartupReconciliation` method, which is wrapped with `manager.RunnableFunc` and fed into the controller-runtime `Manager`'s `Add` method during startup.

## Namespace Controller

[[Code]](../../internal/controller/namespace.go)

The namespace controller watches changes to namespaces on the onboarding cluster. If a namespace is modified, the controller lists all tracked resources within the namespace for which the modification could be relevant, and then triggers reconciliations for them on the tracked resource controller.

A modification on the namespace is deemed 'relevant' for a tracked resource if the corresponding entry in the config's `resourcesToTrack` list either uses a namespace selector, or has any traits defined with a path starting with `.namespace`. In the former case, the modification could have made the namespace match or not match the selector, in the latter one, a tracked trait might have changed.

Neither newly created nor deleted namespaces can contain tracked resources, therefore only update events are taken into account by this controller, create and delete events are ignored.

## Garbage Collector

[[Code]](../../internal/controller/garbage_collector.go)

The garbage collector is not a controller and it does not watch anything. It runs periodically and deletes old `ResourceUsage` objects, as defined in the garbage collection config.

The config controller updates the garbage collection config in the shared information helper whenever it reconciles the `UsageServiceConfig`. This also triggers the garbage collector, which then checks whether it needs to run now (based on the new configuration and the time of the last garbage collection). 

It uses a `time.Ticker` to wait for the next scheduled garbage collection.

The garbage collector implements the `manager.Runnable` interface and is passed into the controller-runtime's `Manager` during startup.

## Shared Information

[[Code]](../../internal/shared/shared.go)

The shared information helper takes care of communication between the different controllers. It is designed as a singleton, so `shared.SharedInformation()` will always return the same instance.

This is not a generic communication channel, but a specialized helper which exposes setters and getters for all information any of the controllers needs to pass to or get from another controller.

All methods require the same lock to be held to ensure thread safety.

It takes, stores, and provides the following information:
- Resources for which usage should be tracked, and active informers for these resources.
  - Written by the config controller, read by the tracked resource controller.
- Manual reconciliation triggers for the tracked resource controller.
- Garbage collection config, and a trigger to have the garbage collector react on a config change.
  - Written by the config controller, read by the garbage collector.

#### Startup Race

One problem of this approach is that the tracked resource controller does not fetch the config itself, but relies on the information in the shared information helper. This leads to a race condition during startup: If the tracked resource controller reconciles a resource before the config controller has read the config the first time - this can happen due to the startup reconciliation logic of the tracked resource controller - the shared information helper has not received any configuration yet and will therefore always return nil. A nil configuration for a given resource is interpreted as 'this resource does not need to be tracked' by the tracked resource controller, which in turn causes it to mark the resource's usage as having ended in the corresponding `ResourceUsage` object. This is not desired if the resource should still be tracked according to the config.

To solve this race, the shared information helper also stores the information whether it has received the config at least once (hidden in the `initialized` variable) and the tracked resource controller returns an error on all reconciliations if that has not yet happened. This is technically not an error, but we return one to abuse the controller-runtime's exponential backoff logic.
