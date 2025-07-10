package usage

import (
	"context"
	"errors"
	"time"

	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/logging"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
	"github.com/openmcp-project/usage-operator/internal/helper"
)

type UsageTracker struct {
	client client.Client
	log    *logging.Logger
}

func NewUsageTracker(log *logging.Logger, client client.Client) (*UsageTracker, error) {
	return &UsageTracker{
		log:    log,
		client: client,
	}, nil

}

func (u *UsageTracker) initLogger(name, project, workspace, mcp_name string) logging.Logger {
	return u.log.WithName("tracker-"+name).WithValues(
		"project", project,
		"workspace", workspace,
		"mcp", mcp_name,
	)
}

func (u *UsageTracker) CreateOrUpdateEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	log := u.initLogger("creation-update", project, workspace, mcp_name)

	objectKey := GetObjectKey(project, workspace, mcp_name)

	var mcpUsage v1.MCPUsage
	err := u.client.Get(ctx, objectKey, &mcpUsage)
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("error at getting MCPUsage resource for %v: %w", mcp_name, err)
	}

	mcpUsage.Name = objectKey.Name
	mcpUsage.Namespace = objectKey.Namespace

	if k8serrors.IsNotFound(err) { // element does not exist, we need to create it
		log.Debug("no mcp usage element found. Creating a new one", "objectKey", objectKey)

		now := metav1.NewTime(time.Now().UTC())
		mcpUsage = v1.MCPUsage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      objectKey.Name,
				Namespace: objectKey.Namespace,
			},
			Spec: v1.MCPUsageSpec{
				Project:           project,
				Workspace:         workspace,
				MCP:               mcp_name,
				Usage:             []v1.DailyUsage{},
				LastUsageCaptured: now,
				MCPCreatedAt:      now,
			},
		}

		err = u.client.Create(ctx, &mcpUsage)
		if err != nil {
			return fmt.Errorf("error when creating MCPUsage resource: %w", err)
		}
	} else {
		// check if mcpUsage element wants to be deleted
		if !mcpUsage.Spec.MCPDeletedAt.IsZero() {
			log.Debug("mcp was deleted in the past, update last usage captured and proceed")
			// MCP was deleted, now created with the same name, update lastUsageCapture
			mcpUsage.Spec.LastUsageCaptured = metav1.NewTime(time.Now().UTC())
			err = u.client.Update(ctx, &mcpUsage)
			if err != nil {
				return fmt.Errorf("error when updating status for MCPUsage resource: %w", err)
			}
		} else {
			// event was fired one time to much? do nothing and return later
			log.Debug("create or update event was fired again but MCPUsage is already valid, ignore it")
		}

	}

	log.Debug("update charging target for mcpusage element")
	// ALWAYS: Check charging target and override it to make sure always the latest charging target is there.
	err = u.UpdateChargingTarget(ctx, &mcpUsage)
	if err != nil {
		return fmt.Errorf("error when updating charging target: %w", err)
	}

	return nil
}

func (u *UsageTracker) UpdateChargingTarget(ctx context.Context, mcpUsage *v1.MCPUsage) error {
	var project, workspace, mcp_name = mcpUsage.Spec.Project, mcpUsage.Spec.Workspace, mcpUsage.Spec.MCP
	log := u.initLogger("update-charging-target", project, workspace, mcp_name)

	chargingTarget, err := helper.ResolveChargingTarget(ctx, u.client, project, workspace, mcp_name)
	if err != nil {
		log.Error(err, fmt.Sprintf("error when resolving charging target %s %s %s", project, workspace, mcp_name))
		mcpUsage.Spec.Message = "error when resolving charging target"
		chargingTarget = "missing"
	}
	if chargingTarget == "" {
		chargingTarget = "missing"
		mcpUsage.Spec.Message = "no charging target specified"
	}
	mcpUsage.Spec.ChargingTarget = chargingTarget

	err = u.client.Update(ctx, mcpUsage)
	if err != nil {
		return fmt.Errorf("error at updating MCPUsage status resource for %s %s %s: %w", project, workspace, mcp_name, err)
	}

	return nil
}

func (u *UsageTracker) DeletionEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	_ = u.initLogger("deletion", project, workspace, mcp_name)

	objectKey := GetObjectKey(project, workspace, mcp_name)
	var mcpUsage = v1.MCPUsage{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectKey.Name,
			Namespace: objectKey.Namespace,
		},
		Spec: v1.MCPUsageSpec{
			MCPDeletedAt: metav1.NewTime(time.Now().UTC()),
		},
	}
	err := u.client.Patch(ctx, &mcpUsage, client.Merge)
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error when setting deletion timestamp on MCPUsage element: %w", err)
	}

	return nil
}

func (u *UsageTracker) ScheduledEvent(ctx context.Context) error {
	log := u.log.WithName("scheduled")
	var mcpUsages v1.MCPUsageList
	err := u.client.List(ctx, &mcpUsages)
	if err != nil {
		return fmt.Errorf("error when getting list of mcp usages: %w", err)
	}

	now := time.Now().UTC()

	var errs error
	for _, mcpUsage := range mcpUsages.Items {
		if !mcpUsage.Spec.MCPDeletedAt.IsZero() {
			// mcp does not exist anymore
			continue
		}

		var project, workspace, mcp_name = mcpUsage.Spec.Project, mcpUsage.Spec.Workspace, mcpUsage.Spec.MCP
		log = log.WithValues(
			"project", project,
			"workspace", workspace,
			"mcp", mcp_name,
		)

		usages := calculateUsage(now, mcpUsage.Spec.LastUsageCaptured.Time)
		usages = MergeDailyUsages(usages, mcpUsage.Spec.Usage)

		mcpUsage.Spec.Usage = usages
		mcpUsage.Spec.LastUsageCaptured = metav1.NewTime(now)
		err = u.client.Update(ctx, &mcpUsage)
		errs = errors.Join(errs, err)
	}

	if errs != nil {
		return fmt.Errorf("error when updating the usage: %w", errs)
	}

	return nil
}

func (u *UsageTracker) GarbageCollection(ctx context.Context) error {
	_ = u.log.WithName("scheduled")

	var mcpUsages v1.MCPUsageList
	err := u.client.List(ctx, &mcpUsages)
	if err != nil {
		return fmt.Errorf("error when getting list of mcp usages: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Hour * 24)
	oneMonth := time.Hour * 24 * 32
	latestTimestamp := now.Add(-oneMonth)

	var errs error
	for _, mcpUsage := range mcpUsages.Items {
		usagesToKeep := make([]v1.DailyUsage, 0, len(mcpUsage.Spec.Usage))
		for _, usage := range mcpUsage.Spec.Usage {
			if !usage.Date.Time.Before(latestTimestamp) {
				usagesToKeep = append(usagesToKeep, usage)
			}
		}
		mcpUsage.Spec.Usage = usagesToKeep
		err = u.client.Update(ctx, &mcpUsage)
		errs = errors.Join(errs, err)
	}

	if errs != nil {
		return fmt.Errorf("error when updating the usage: %w", errs)
	}

	return nil
}
