package usage

import (
	"context"
	"errors"
	"time"

	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
	"github.com/openmcp-project/usage-operator/internal/helper"
)

type UsageTracker struct {
	client client.Client
}

func NewUsageTracker(client client.Client) (*UsageTracker, error) {
	return &UsageTracker{
		client: client,
	}, nil
}

func (u *UsageTracker) initLogger(ctx context.Context, name, project, workspace, mcp_name string) logr.Logger {
	log := logf.FromContext(ctx)

	return log.WithName(name).WithValues(
		"project", project,
		"workspace", workspace,
		"mcp", mcp_name,
	)
}

func (u *UsageTracker) CreateOrUpdateEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	log := u.initLogger(ctx, "creation-update", project, workspace, mcp_name)

	objectKey, err := GetObjectKey(project, workspace, mcp_name)
	if err != nil {
		return fmt.Errorf("error getting object key: %w", err)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mcpUsage v1.MCPUsage
		err = u.client.Get(ctx, objectKey, &mcpUsage)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("error at getting MCPUsage resource for %v: %w", mcp_name, err)
		}

		mcpUsage.Name = objectKey.Name

		if k8serrors.IsNotFound(err) { // element does not exist, we need to create it
			log.Info("no mcp usage element found. Creating a new one", "objectKey", objectKey)

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
				log.Info("mcp was deleted in the past, update last usage captured and proceed")
				// MCP was deleted, now created with the same name, update lastUsageCapture
				mcpUsage.Spec.LastUsageCaptured = metav1.NewTime(time.Now().UTC())
				err = u.client.Update(ctx, &mcpUsage)
				if err != nil {
					if k8serrors.IsConflict(err) {
						log.Info("conflict detected when updating resource", "MCPUsage", mcpUsage.Name)
						return err
					}
					return fmt.Errorf("error when updating status for MCPUsage resource: %w", err)
				}
			} else {
				// event was fired one time to much? do nothing and return later
				log.Info("create or update event was fired again but MCPUsage is already valid, ignore it")
			}

		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error when updating mcp usage resource: %w", err)
	}

	log.Info("update charging target for mcpusage element")
	// ALWAYS: Check charging target and override it to make sure always the latest charging target is there.
	err = u.UpdateChargingTarget(ctx, project, workspace, mcp_name)
	if err != nil {
		return fmt.Errorf("error when updating charging target: %w", err)
	}

	return nil
}

func (u *UsageTracker) UpdateChargingTarget(ctx context.Context, project string, workspace string, mcp_name string) error {
	log := u.initLogger(ctx, "charging_target", project, workspace, mcp_name)

	objectKey, err := GetObjectKey(project, workspace, mcp_name)
	if err != nil {
		return fmt.Errorf("error getting object key: %w", err)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var mcpUsage v1.MCPUsage
		err = u.client.Get(ctx, objectKey, &mcpUsage)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("error at getting MCPUsage resource for %v: %w", mcp_name, err)
		}

		chargingTarget, chargingTargetType, err := helper.ResolveChargingTarget(ctx, u.client, project, workspace, mcp_name)
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
		mcpUsage.Spec.ChargingTargetType = chargingTargetType

		err = u.client.Update(ctx, &mcpUsage)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("Conflict detected for MCPUsage, retrying...", "MCPUsageName", mcpUsage.Name)
				return err
			}
			return fmt.Errorf("error at updating MCPUsage status resource for %s %s %s: %w", project, workspace, mcp_name, err)
		}

		return nil
	})

	return err
}

func (u *UsageTracker) DeletionEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	_ = u.initLogger(ctx, "deletion", project, workspace, mcp_name)

	objectKey, err := GetObjectKey(project, workspace, mcp_name)
	if err != nil {
		return fmt.Errorf("error getting object key: %w", err)
	}

	var mcpUsage v1.MCPUsage
	err = u.client.Get(ctx, objectKey, &mcpUsage)
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error getting MCPUsage resource: %w", err)
	}

	mcpUsage.Spec.MCPDeletedAt = metav1.NewTime(time.Now().UTC())
	err = u.client.Update(ctx, &mcpUsage)
	if err != nil {
		return fmt.Errorf("error when setting deletion timestamp on MCPUsage element: %w", err)
	}

	return nil
}

func (u *UsageTracker) ScheduledEvent(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("scheduled")

	var mcpUsages v1.MCPUsageList
	err := u.client.List(ctx, &mcpUsages)
	if err != nil {
		return fmt.Errorf("error when getting list of mcp usages: %w", err)
	}

	now := time.Now().UTC()

	var errs error
	for _, mcpUsage := range mcpUsages.Items {
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var project, workspace, mcp_name = mcpUsage.Spec.Project, mcpUsage.Spec.Workspace, mcpUsage.Spec.MCP
			log = log.WithValues(
				"project", project,
				"workspace", workspace,
				"mcp", mcp_name,
			)

			err = u.client.Get(ctx, client.ObjectKey{
				Name: mcpUsage.Name,
			}, &mcpUsage)
			if err != nil {
				return err
			}

			if !mcpUsage.Spec.MCPDeletedAt.IsZero() {
				// mcp does not exist anymore
				return nil
			}

			usages := calculateUsage(now, mcpUsage.Spec.LastUsageCaptured.Time)
			usages = MergeDailyUsages(usages, mcpUsage.Spec.Usage)

			mcpUsage.Spec.Usage = usages
			mcpUsage.Spec.LastUsageCaptured = metav1.NewTime(now)
			err = u.client.Update(ctx, &mcpUsage)
			if err != nil {
				if k8serrors.IsConflict(err) {
					log.Error(err, "Conflict detected for McpUsage, retrying...\n", "mcpUsage", mcpUsage.Name)
					return err
				}
				return fmt.Errorf("failed to update McpUsage %s: %w", mcpUsage.Name, err)
			}

			return nil
		})

		if err != nil {
			errs = errors.Join(errs, err)
		}
	}

	if errs != nil {
		return fmt.Errorf("error when updating the usage: %w", errs)
	}

	return nil
}

func (u *UsageTracker) GarbageCollection(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("garbage")

	var mcpUsages v1.MCPUsageList
	err := u.client.List(ctx, &mcpUsages)
	if err != nil {
		return fmt.Errorf("error when getting list of mcp usages: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Hour * 24)
	oneMonth := time.Hour * 24 * 32
	latestTimestamp := now.Add(-oneMonth)

	log.Info("garbage collect old entries", "before", latestTimestamp)

	var errs error
	for _, mcpUsage := range mcpUsages.Items {
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			err = u.client.Get(ctx, client.ObjectKey{
				Name: mcpUsage.Name,
			}, &mcpUsage)
			if err != nil {
				return err
			}

			usagesToKeep := make([]v1.DailyUsage, 0, len(mcpUsage.Spec.Usage))
			for _, usage := range mcpUsage.Spec.Usage {
				if !usage.Date.Time.Before(latestTimestamp) {
					usagesToKeep = append(usagesToKeep, usage)
				}
			}
			mcpUsage.Spec.Usage = usagesToKeep
			err = u.client.Update(ctx, &mcpUsage)
			if err != nil {
				if k8serrors.IsConflict(err) {
					log.Error(err, "Conflict detected for McpUsage, retrying...\n", "mcpUsage", mcpUsage.Name)
					return err
				}
				return fmt.Errorf("failed to update McpUsage %s: %w", mcpUsage.Name, err)
			}

			return nil
		})

		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("error when updating the mcp usage resource: %w", err))
		}
	}

	if errs != nil {
		return fmt.Errorf("error when updating the usage: %w", errs)
	}

	return nil
}
