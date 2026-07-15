package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/shared"
	usageutil "github.com/openmcp-project/usage-operator/internal/usage"
)

const (
	TrackedResourceControllerName = "TrackedResource"
)

// TypedRequest is like reconcile.Request, but includes the GVK of the resource being reconciled.
type TypedRequest struct {
	types.NamespacedName
	schema.GroupVersionKind
}

type TrackedResourceController struct {
	OnboardingCluster *clusters.Cluster
	internal          controller.TypedController[TypedRequest]
}

func NewTrackedResourceController(onboardingCluster *clusters.Cluster) *TrackedResourceController {
	return &TrackedResourceController{
		OnboardingCluster: onboardingCluster,
	}
}

var _ reconcile.TypedReconciler[TypedRequest] = &TrackedResourceController{}

func (c *TrackedResourceController) Reconcile(ctx context.Context, req TypedRequest) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(TrackedResourceControllerName).WithName(req.Kind).WithValues("group", req.Group, "version", req.Version, "kind", req.Kind, "name", req.Name, "namespace", req.Namespace)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	res, err := c.reconcile(ctx, req)
	if err == nil && res.RequeueAfter > 0 {
		log.Debug("Requeuing object", "requeueAfter", res.RequeueAfter, "requeueAt", time.Now().Add(res.RequeueAfter))
	}
	return res, err
}

func (c *TrackedResourceController) reconcile(ctx context.Context, req TypedRequest) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	// abort reconciliation if the config controller has not reconciled successfully at least once since the last restart of the operator
	// otherwise, the fetched config is always empty, causing this controller to stop all resource tracking, which is not what we want
	if !shared.SharedInformation().IsInitialized() {
		// this is technically not an error, we are just abusing the controller-runtime's exponential backoff mechanism to postpone the reconciliation until the config controller has reconciled successfully at least once
		return reconcile.Result{}, fmt.Errorf("unable to reconcile resource %s (%s), waiting for config controller to initialize configuration", req.NamespacedName.String(), req.GroupVersionKind.String())
	}

	// fetch resource tracking information
	ut := shared.SharedInformation().GetWatch(req.GroupVersionKind)
	if ut == nil {
		log.Debug("Received reconciliation trigger for a resource type which is not being tracked, this can happen if the resource type was removed from the configuration")
	}

	// fetch resource
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(req.GroupVersionKind)
	if err := c.OnboardingCluster.Client().Get(ctx, req.NamespacedName, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("error fetching resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
		}
		obj = nil // resource was deleted
	}

	selected := false
	var ns *corev1.Namespace
	if ut == nil {
		// TODO: Do we need to do something in this case?
	} else {
		// fetch namespace, if required
		if ut.NamespaceRequired() {
			ns = &corev1.Namespace{}
			if err := c.OnboardingCluster.Client().Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
				return reconcile.Result{}, fmt.Errorf("error fetching namespace %s for resource %s (%s): %w", req.Namespace, req.NamespacedName.String(), req.GroupVersionKind.String(), err)
			}
		}

		// check whether resource passes the configured selectors (if any)
		if obj != nil {
			var err error
			selected, err = ut.MatchesSelector(ctx, obj, ns)
			if err != nil {
				log.Error(err, "Error matching resource against selector")
			}
		}
	}

	if ut == nil || !selected || (ut.Config.TrackUntil == usagev1alpha1.TrackUntilDeletionTimestamp && !obj.GetDeletionTimestamp().IsZero()) {
		// This means that we should stop tracking the resource.
		// Possible reasons:
		// - config has changed to not include the resource kind anymore
		// - either config or resource has changed to not match the selector anymore
		// - resource has been deleted (or got a deletion timestamp, depending on the config)
		return c.handleStopTracking(ctx, ut, req)
	}

	return c.handleTracking(ctx, ut, req, obj, ns)
}

func (c *TrackedResourceController) handleTracking(ctx context.Context, ut *usageutil.UsageTracker, req TypedRequest, obj *unstructured.Unstructured, ns *corev1.Namespace) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Tracking resource") // only print this at debug level, because it will happen very frequently (on every change to any watched resource)

	now := time.Now().UTC().Truncate(time.Minute)
	traitData, err := ut.TraitsExtractor.ExtractTraits(obj, ns)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error extracting traits for resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}

	newTrackingDurationStart := obj.GetCreationTimestamp().Time.Truncate(time.Minute)
	rus, err := c.fetchResourceUsages(ctx, req, now)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error fetching ResourceUsage objects for resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}

	if len(rus) > 0 {
		last := rus[0]
		// We cannot use client.Patch here, because when updating the end time for a trait with a 'null' value, the patch will contain 'value: null',
		// which will be interpreted as "delete the value" instead of "set the value to null". So we need to use client.Update, which will set the value to null correctly.

		// check if we need a new ResourceUsage object (if the last one has ended)
		if now.Before(last.Spec.TrackingPeriod.End.Time) {
			// latest ResourceUsage is still active, track usage in it
			log.Debug("Tracking in ongoing ResourceUsage object", "resourceUsage", last.Name)
			ut.Track(last, obj, traitData, now)
			if err := c.OnboardingCluster.Client().Update(ctx, last); err != nil {
				return reconcile.Result{}, fmt.Errorf("error tracking usage for ResourceUsage %s: %w", last.Name, err)
			}
			if last.Status.Phase != usagev1alpha1.UsagePhaseOngoing {
				last.Status.Phase = usagev1alpha1.UsagePhaseOngoing
				if err := c.OnboardingCluster.Client().Status().Update(ctx, last); err != nil {
					return reconcile.Result{}, fmt.Errorf("error updating status for ResourceUsage %s: %w", last.Name, err)
				}
			}
			return RequeueAtTrackingPeriodEnd(last), nil
		}

		// latest ResourceUsage has ended
		if last.Status.Phase != usagev1alpha1.UsagePhaseCompleted {
			// latest ResourceUsage needs to be completed before we can create a new one
			log.Info("Latest ResourceUsage's tracking period has ended, completing it", "expirationTime", last.Spec.TrackingPeriod.End.Format(time.RFC3339))
			ut.CompleteResourceUsage(last)
			statusBackup := last.Status.DeepCopy()
			if err := c.OnboardingCluster.Client().Update(ctx, last); err != nil {
				return reconcile.Result{}, fmt.Errorf("error completing ResourceUsage %s (spec): %w", last.Name, err)
			}
			last.Status = *statusBackup
			if err := c.OnboardingCluster.Client().Status().Update(ctx, last); err != nil {
				return reconcile.Result{}, fmt.Errorf("error completing ResourceUsage %s (status): %w", last.Name, err)
			}
		}
		newTrackingDurationStart = last.Spec.TrackingPeriod.End.Time.Truncate(time.Minute)
	}

	// create new ResourceUsage object
	// We are using some heuristics here to avoid 'holes' in the tracking due to delays in the reconciliation.
	// Basically, we assume the object to have existed unchanged (like it is now) since the end of the last tracking period, or its creation time, whichever is later.
	// While this should be correct for the object itself, it might not be correct for the traits, which could have changed in the meantime. We expect the traits to only change rarely though, so this should be a reasonable approximation.
	// As a safeguard, we dismiss this heuristic, if it would date the starting time back further than the configured ResourceUsage period, leading to a ResourceUsage object which would immediately needed to be completed again.
	if now.Sub(newTrackingDurationStart) > ut.Config.ResourceUsagePeriod.Duration {
		newTrackingDurationStart = now
	}
	ru := ut.NewResourceUsage(obj, traitData, newTrackingDurationStart)
	if err := c.OnboardingCluster.Client().Create(ctx, ru); err != nil {
		return reconcile.Result{}, fmt.Errorf("error creating new ResourceUsage for resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}
	log.Debug("Created new ResourceUsage object", "resourceUsage", ru.Name)
	old := ru.DeepCopy()
	ru.Status.Phase = usagev1alpha1.UsagePhaseOngoing
	if err := c.OnboardingCluster.Client().Status().Patch(ctx, ru, client.MergeFrom(old)); err != nil {
		return reconcile.Result{}, fmt.Errorf("error updating status for ResourceUsage %s: %w", ru.Name, err)
	}
	return RequeueAtTrackingPeriodEnd(ru), nil
}

func (c *TrackedResourceController) handleStopTracking(ctx context.Context, ut *usageutil.UsageTracker, req TypedRequest) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Stopping tracking for resource")

	now := time.Now().Truncate(time.Minute)
	rus, err := c.fetchResourceUsages(ctx, req, now)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error fetching ResourceUsage objects for resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}

	if len(rus) == 0 {
		// no ResourceUsage exists, nothing to do
		log.Debug("No ResourceUsage found, nothing to do")
		return reconcile.Result{}, nil
	}

	last := rus[0]
	if last.Status.Phase == usagev1alpha1.UsagePhaseCompleted {
		log.Debug("Latest ResourceUsage is already completed, nothing to do")
		return reconcile.Result{}, nil
	}

	// We cannot use client.Patch here, because when updating the end time for a trait with a 'null' value, the patch will contain 'value: null',
	// which will be interpreted as "delete the value" instead of "set the value to null". So we need to use client.Update, which will set the value to null correctly.

	if !now.Before(last.Spec.TrackingPeriod.End.Time) {
		log.Info("Latest ResourceUsage's tracking period has ended, completing it", "expirationTime", last.Spec.TrackingPeriod.End.Format(time.RFC3339))
		ut.CompleteResourceUsage(last)
		statusBackup := last.Status.DeepCopy()
		if err := c.OnboardingCluster.Client().Update(ctx, last); err != nil {
			return reconcile.Result{}, fmt.Errorf("error completing ResourceUsage %s (spec) for resource %s (%s): %w", last.Name, req.NamespacedName.String(), req.GroupVersionKind.String(), err)
		}
		last.Status = *statusBackup
		if err := c.OnboardingCluster.Client().Status().Update(ctx, last); err != nil {
			return reconcile.Result{}, fmt.Errorf("error completing ResourceUsage %s (status) for resource %s (%s): %w", last.Name, req.NamespacedName.String(), req.GroupVersionKind.String(), err)
		}
		return reconcile.Result{}, nil
	}

	ut.StopTracking(last, now)
	if err := c.OnboardingCluster.Client().Update(ctx, last); err != nil {
		return reconcile.Result{}, fmt.Errorf("error stopping tracking for ResourceUsage %s for resource %s (%s): %w", last.Name, req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}

	// tracking is stopped, but requeue the reconciliation at the end of the tracking period to complete the ResourceUsage object
	return RequeueAtTrackingPeriodEnd(last), nil
}

// fetchResourceUsages fetches all ResourceUsage objects for the given resource from the cluster.
// They are sorted by their tracking period's start time, with the most recent one first.
func (c *TrackedResourceController) fetchResourceUsages(ctx context.Context, req TypedRequest, now time.Time) ([]*usagev1alpha1.ResourceUsage, error) {
	rul := &usagev1alpha1.ResourceUsageList{}
	if err := c.OnboardingCluster.Client().List(ctx, rul, client.MatchingFields{
		"spec.resource.group":     req.Group,
		"spec.resource.version":   req.Version,
		"spec.resource.kind":      req.Kind,
		"spec.resource.name":      req.Name,
		"spec.resource.namespace": req.Namespace,
	}); err != nil {
		return nil, fmt.Errorf("error listing ResourceUsage objects for resource %s (%s): %w", req.NamespacedName.String(), req.GroupVersionKind.String(), err)
	}

	res := make([]*usagev1alpha1.ResourceUsage, 0, len(rul.Items))
	for i := range rul.Items {
		if !rul.Items[i].Spec.TrackingPeriod.Start.IsZero() && !rul.Items[i].Spec.TrackingPeriod.Start.After(now) {
			res = append(res, &rul.Items[i])
		}
	}

	slices.SortStableFunc(res, func(a, b *usagev1alpha1.ResourceUsage) int {
		if a.Spec.TrackingPeriod.Start.IsZero() {
			if b.Spec.TrackingPeriod.Start.IsZero() {
				return 0
			} else {
				return 1
			}
		}
		if b.Spec.TrackingPeriod.Start.IsZero() {
			return -1
		}
		return b.Spec.TrackingPeriod.Start.Compare(a.Spec.TrackingPeriod.Start.Time)
	})

	return res, nil
}

// RequeueAtTrackingPeriodEnd returns a reconcile.Result that will requeue the reconciliation at the end of the tracking period of the given ResourceUsage object.
// If the ResourceUsage is nil, or its tracking period has no end time, or the end time is in the past, it returns an empty reconcile.Result (no requeue).
// An offset of one second is added to the requeue time to ensure that the tracking period has ended when the next reconciliation is triggered.
func RequeueAtTrackingPeriodEnd(usage *usagev1alpha1.ResourceUsage) reconcile.Result {
	now := time.Now()
	if usage == nil || usage.Spec.TrackingPeriod.End.IsZero() || usage.Spec.TrackingPeriod.End.Time.Before(now) {
		return reconcile.Result{}
	}
	return reconcile.Result{RequeueAfter: usage.Spec.TrackingPeriod.End.Sub(now) + time.Second}
}

// SetupWithManager sets up the controller with the given manager.
// As a side effect, this also registers the manual reconciliation trigger and the function to start informers for specific GVKs in the shared information instance.
func (c *TrackedResourceController) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	c.internal, err = controller.NewTyped(TrackedResourceControllerName, mgr, controller.TypedOptions[TypedRequest]{Reconciler: c})
	if err != nil {
		return fmt.Errorf("error creating typed controller: %w", err)
	}

	trigger := make(chan event.TypedGenericEvent[*unstructured.Unstructured], 1024)
	if err := c.internal.Watch(source.TypedChannel(trigger, handler.TypedFuncs[*unstructured.Unstructured, TypedRequest]{
		GenericFunc: func(ctx context.Context, e event.TypedGenericEvent[*unstructured.Unstructured], q workqueue.TypedRateLimitingInterface[TypedRequest]) {
			if e.Object != nil {
				q.Add(TypedRequest{
					NamespacedName:   client.ObjectKeyFromObject(e.Object),
					GroupVersionKind: e.Object.GroupVersionKind(),
				})
			}
		},
	})); err != nil {
		return fmt.Errorf("error setting up watch for manual reconciliation trigger: %w", err)
	}
	shared.SharedInformation().SetReconcileTrigger(trigger)

	shared.SharedInformation().SetStartInformerFunc(func(gvk schema.GroupVersionKind) error {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		return c.internal.Watch(source.TypedKind(c.OnboardingCluster.Cluster().GetCache(), u, handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj *unstructured.Unstructured) []TypedRequest {
			return []TypedRequest{{
				NamespacedName:   types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()},
				GroupVersionKind: gvk,
			}}
		})))
	})
	return nil
}

// StartupReconciliation is meant to trigger a reconciliation for all resources with non-completed ResourceUsage objects at the startup of the operator.
// This includes especially those, which are not watched anymore due to a config change. Their corresponding ResourceUsage objects would otherwise never be completed.
// Note that this function IS NOT MEANT TO BE CALLED MANUALLY. Instead, it should be passed into mgr.Add(manager.RunnableFunc(<here>)) to be executed at the startup of the operator.
// The controller's SetupWithManager function must be called before this function, so that the manual reconciliation trigger is set up in the shared information instance.
func (c *TrackedResourceController) StartupReconciliation(ctx context.Context) error {
	rus := &usagev1alpha1.ResourceUsageList{}
	if err := c.OnboardingCluster.Client().List(ctx, rus); err != nil {
		return fmt.Errorf("error listing ResourceUsage objects: %w", err)
	}

	for _, ru := range rus.Items {
		if ru.Status.Phase != usagev1alpha1.UsagePhaseOngoing {
			continue
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   ru.Spec.Resource.Group,
			Version: ru.Spec.Resource.Version,
			Kind:    ru.Spec.Resource.Kind,
		})
		obj.SetName(ru.Spec.Resource.Name)
		obj.SetNamespace(ru.Spec.Resource.Namespace)
		shared.SharedInformation().TriggerReconcile(obj)
	}
	return nil
}
