package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	"github.com/openmcp-project/usage-operator/internal/shared"
)

const (
	NamespaceControllerName = "NamespaceController"
)

// NamespaceController is a small controller that watches namespaces and triggers reconciliations for tracked resources within a changed namespace (if changes to the namespace could be relevant for the tracked resources).
type NamespaceController struct {
	OnboardingCluster *clusters.Cluster
}

func NewNamespaceController(onboardingCluster *clusters.Cluster) *NamespaceController {
	return &NamespaceController{
		OnboardingCluster: onboardingCluster,
	}
}

func (c *NamespaceController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(NamespaceControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	return c.reconcile(ctx, req)
}

func (c *NamespaceController) reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	// fetch namespace
	ns := &corev1.Namespace{}
	if err := c.OnboardingCluster.Client().Get(ctx, req.NamespacedName, ns); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Namespace not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error fetching Namespace %s: %w", req.Name, err)
	}

	// get all resource GVKs that are tracked for which a change in this namespace could be relevant
	relevantTrackers := shared.SharedInformation().GetWatchesForNamespaceUpdate()

	// list all resources of the relevant GVKs in this namespace and trigger a reconcile for each of them
	// we do not evaluate the configured namespace selector here, as we might also need to reconcile resources in a non-matching namespace (to stop tracking them)
	var errs error
	recCounter := 0
	for _, ut := range relevantTrackers {
		gvkLog := log.WithValues("group", ut.Config.Group, "version", ut.Config.Version, "kind", ut.Config.Kind)

		// list all resources of the relevant GVK in this namespace
		resources := &unstructured.UnstructuredList{}
		resources.SetGroupVersionKind(schema.GroupVersionKind(ut.Config.GroupVersionKind))
		if err := c.OnboardingCluster.Client().List(ctx, resources, client.InNamespace(ns.Name)); err != nil {
			errs = errors.Join(errs, fmt.Errorf("error listing resources for %s in namespace %s: %w", ut.Config.String(), ns.Name, err))
			continue
		}
		if len(resources.Items) > 0 {
			gvkLog.Info("Triggering resource reconciliations", "count", len(resources.Items))
			for _, res := range resources.Items {
				res.SetGroupVersionKind(schema.GroupVersionKind(ut.Config.GroupVersionKind))
				shared.SharedInformation().TriggerReconcile(&res)
			}
			recCounter += len(resources.Items)
		}
	}
	log.Info("Triggered reconciliations for resources in namespace in total", "count", recCounter)
	if errs != nil {
		return reconcile.Result{}, fmt.Errorf("one or more errors occurred trying to trigger reconciliations for tracked resources in namespace '%s': %w", ns.Name, errs)
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *NamespaceController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch Namespace resources
		For(&corev1.Namespace{}, builder.WithPredicates(
			// only changes to namespaces are relevant, because neither freshly created nor deleted namespaces can contain any tracked resources
			ctrlutils.OnUpdatePredicate(),
		)).
		Complete(c)
}
