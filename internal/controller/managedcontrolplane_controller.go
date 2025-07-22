/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"regexp"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	corev1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/usage-operator/internal/usage"
)

// ManagedControlPlaneReconciler reconciles a ManagedControlPlane object
type ManagedControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	UsageTracker *usage.UsageTracker
}

// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.openmcp.cloud,resources=managedcontrolplanes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ManagedControlPlane object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ManagedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log, err := logging.FromContext(ctx)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var mcp corev1alpha1.ManagedControlPlane
	if err := r.Get(ctx, req.NamespacedName, &mcp); err != nil {
		log.Error(err, "unable to fetch mcp")

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	re := regexp.MustCompile("project-(.+)--ws-(.+)")
	namespace := mcp.Namespace
	matches := re.FindStringSubmatch(namespace)
	if len(matches) != 3 {
		err := errors.New("namespace of mcp is invalid")
		log.Error(err, "namespace of mcp is invalid")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	project := matches[1]
	workspace := matches[2]

	log.Info("mcp '" + mcp.Name + "' status '" + string(mcp.Status.Status) + "'")

	if mcp.GetDeletionTimestamp() != nil || mcp.Status.Status == corev1alpha1.MCPStatusDeleting {
		log.Info("mcp '" + mcp.Name + "' was deleted. Tracking it...")
		err := r.UsageTracker.DeletionEvent(ctx, project, workspace, mcp.Name)
		if err != nil {
			log.Error(err, "error when tracking deletion")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, nil
	}

	err = r.UsageTracker.CreateOrUpdateEvent(ctx, project, workspace, mcp.Name)
	if err != nil {
		log.Error(err, "error when tracking create or ignore of mcp")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.ManagedControlPlane{}).
		Named("managedcontrolplane").
		Complete(r)
}
