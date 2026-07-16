package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/shared"
	usageutil "github.com/openmcp-project/usage-operator/internal/usage"
)

const (
	ConfigControllerName = "UsageConfigController"

	EventActionReconcile    = "Reconcile"
	EventReasonWatchStarted = "WatchStarted"
	EventReasonWatchStopped = "WatchStopped"
)

type ConfigController struct {
	PlatformCluster   *clusters.Cluster
	OnboardingCluster *clusters.Cluster
	er                events.EventRecorder
	ProviderName      string
	initialized       bool
}

func NewConfigController(platformCluster, onboardingCluster *clusters.Cluster, providerName string, er events.EventRecorder) *ConfigController {
	return &ConfigController{
		PlatformCluster:   platformCluster,
		OnboardingCluster: onboardingCluster,
		er:                er,
		ProviderName:      providerName,
	}
}

func (c *ConfigController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ConfigControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	cfg, rr, err := c.reconcile(ctx, req)
	if c.er != nil {
		if err != nil {
			if cfg != nil {
				c.er.Eventf(cfg, nil, corev1.EventTypeWarning, "ReconcileError", EventActionReconcile, "Reconcile failed: %v", err)
			}
		} else {
			if cfg != nil {
				c.er.Eventf(cfg, nil, corev1.EventTypeNormal, "ReconcileSuccess", EventActionReconcile, "Reconcile successful")
			}
		}
	}
	if !c.initialized && err == nil {
		shared.SharedInformation().SetInitialized()
		c.initialized = true
	}

	return rr, err
}

// nolint:unparam
func (c *ConfigController) reconcile(ctx context.Context, req reconcile.Request) (*usagev1alpha1.UsageServiceConfig, reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	if req.Name != c.ProviderName {
		log.Info("Skipping reconciliation because the config belongs to a different instance of this platform service", "providerName", c.ProviderName)
		return nil, reconcile.Result{}, nil
	}

	// fetch config
	cfg := &usagev1alpha1.UsageServiceConfig{}
	if err := c.PlatformCluster.Client().Get(ctx, req.NamespacedName, cfg); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Config resource not found, disabling tracking for all resource types")
			shared.SharedInformation().ClearWatches()
			shared.SharedInformation().SetGarbageCollectionConfig(nil)
			return nil, reconcile.Result{}, nil
		}
		return nil, reconcile.Result{}, fmt.Errorf("error fetching UsageServiceConfig %s: %w", req.String(), err)
	}

	// handle operation annotation
	if cfg.GetAnnotations() != nil {
		op, ok := cfg.GetAnnotations()[openmcpconst.OperationAnnotation]
		if ok {
			switch op {
			case openmcpconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return nil, reconcile.Result{}, nil
			case openmcpconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, c.PlatformCluster.Client(), cfg, openmcpconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return nil, reconcile.Result{}, fmt.Errorf("error removing operation annotation: %w", err)
				}
			}
		}
	}

	if !cfg.DeletionTimestamp.IsZero() {
		log.Info("UsageServiceConfig '%s' is in deletion, disabling all resource usage tracking", cfg.Name)
		shared.SharedInformation().ClearWatches()
		shared.SharedInformation().SetGarbageCollectionConfig(nil)
		return nil, reconcile.Result{}, nil
	} else {
		// set garbage collection config
		shared.SharedInformation().SetGarbageCollectionConfig(cfg.Spec.GarbageCollection)
		// sync internal tracking info with the config
		watchesToSet := map[schema.GroupVersionKind]*usageutil.UsageTracker{}
		watchesToUnset := shared.SharedInformation().WatchedGVKs()
		var errs error
		for _, res := range cfg.Spec.ResourcesToTrack {
			gvk := schema.GroupVersionKind(res.GroupVersionKind)
			ut, err := usageutil.NewUsageTracker(ctx, &res)
			if err != nil {
				errs = errors.Join(errs, err)
			}
			watchesToSet[gvk] = ut
			watchesToUnset.Delete(gvk)
		}
		if errs != nil {
			return cfg, reconcile.Result{}, errs
		}
		gvksToReconcile := sets.New[schema.GroupVersionKind]()
		for gvk, ut := range watchesToSet {
			watchedBefore := shared.SharedInformation().GetWatch(gvk) != nil
			rec, err := shared.SharedInformation().SetWatch(gvk, ut)
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("error setting up watch for %s: %w", gvk.String(), err))
			} else if rec {
				gvksToReconcile.Insert(gvk)
			}
			if c.er != nil && !watchedBefore {
				c.er.Eventf(cfg, nil, corev1.EventTypeNormal, EventReasonWatchStarted, EventActionReconcile, "Started watching resource type %s", gvk.String())
			}
		}
		for gvk := range watchesToUnset {
			watchedBefore := shared.SharedInformation().GetWatch(gvk) != nil
			rec, err := shared.SharedInformation().SetWatch(gvk, nil)
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("error stopping watch for %s: %w", gvk.String(), err))
			} else if rec {
				gvksToReconcile.Insert(gvk)
			}
			if c.er != nil && watchedBefore {
				c.er.Eventf(cfg, nil, corev1.EventTypeNormal, EventReasonWatchStopped, EventActionReconcile, "Stopped watching resource type %s", gvk.String())
			}
		}

		// for all GVKs which require reconciliation:
		// list all resources of that GVK and trigger a reconcile for each of them
		// Note that this can potentially block, if the buffer for the manual reconciliation triggers is full.
		if len(gvksToReconcile) > 0 {
			for gvk := range gvksToReconcile {
				gvkLog := log.WithValues("group", gvk.Group, "version", gvk.Version, "kind", gvk.Kind)
				gvkLog.Debug("Listing all resources to trigger reconciliations")
				resources := &unstructured.UnstructuredList{}
				resources.SetGroupVersionKind(gvk)
				if err := c.OnboardingCluster.Retry().List(ctx, resources); err != nil {
					// only log, since retrying the reconciliation would not help here
					gvkLog.Error(err, "Error listing resources")
					continue
				}
				if len(resources.Items) > 0 {
					gvkLog.Debug("Triggering resource reconciliations", "count", len(resources.Items))
					for i := range resources.Items {
						res := &resources.Items[i]
						res.SetGroupVersionKind(gvk)
						shared.SharedInformation().TriggerReconcile(res)
					}
				}
			}
			log.Debug("Triggered reconciliations for all resources with changed watch configurations")
		}

		if errs != nil {
			return cfg, reconcile.Result{}, errs
		}
	}

	return cfg, reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *ConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch UsageServiceConfig resources
		For(&usagev1alpha1.UsageServiceConfig{}, builder.WithPredicates(predicate.And(
			ctrlutils.ExactNamePredicate(c.ProviderName, ""),
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				ctrlutils.DeletionTimestampChangedPredicate{},
				ctrlutils.GotAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(
				ctrlutils.HasAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
		))).
		Complete(c)
}
