package usage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sync"
	"time"

	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openmcp-project/controller-utils/pkg/logging"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
	"github.com/openmcp-project/usage-operator/internal/helper"
)

type UsageTracker struct {
	db   *sql.DB
	lock sync.RWMutex

	log *logging.Logger
}

func NewUsageTracker(log *logging.Logger) (*UsageTracker, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	return &UsageTracker{
		db:  db,
		log: log,
	}, nil

}

func (u *UsageTracker) Close() error {
	return u.db.Close()
}

// This method
// creates a tracking entry in the DB, if it not already exists
// updated a tracking entry in the DB, if it is there, but has a deleted_at entry
// does nothing to the DB, if it is already there
func (u *UsageTracker) CreateOrIgnoreEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	_ = log.FromContext(ctx)

	trackingEntry, err := u.getTrackingEntry(ctx, project, workspace, mcp_name)
	if err != nil {
		return err
	}

	if trackingEntry == nil {
		// Not found an already existing entry
		return u.CreationEvent(ctx, project, workspace, mcp_name)
	}

	if !trackingEntry.DeletedAt.Valid {
		u.lock.Lock()
		defer u.lock.Unlock()

		// Update entry in DB
		sql := "UPDATE mcp SET deleted_at = NULL WHERE project = ? AND workspace = ? AND mcp = ?"
		_, err := u.db.ExecContext(ctx, sql, project, workspace, mcp_name)
		return err
	}

	return nil
}

func (u *UsageTracker) getTrackingEntry(ctx context.Context, project string, workspace string, mcp_name string) (*TrackingMCPEntry, error) {
	u.lock.RLock()
	var trackingEntry TrackingMCPEntry
	query := "SELECT project, workspace, mcp, last_usage_capture, deleted_at FROM mcp WHERE project = ? AND workspace = ? AND mcp = ?"
	row := u.db.QueryRowContext(ctx, query, project, workspace, mcp_name)
	u.lock.RUnlock()

	err := row.Scan(&trackingEntry.Project, &trackingEntry.Workspace, &trackingEntry.Name, &trackingEntry.LastUsageCapture, &trackingEntry.DeletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &trackingEntry, err
}

func (u *UsageTracker) CreationEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	u.lock.Lock()

	creation_timestamp := time.Now().UTC()
	sql := "INSERT INTO mcp (project, workspace, mcp, last_usage_capture) VALUES (?, ?, ?, ?)"
	_, err := u.db.ExecContext(ctx, sql, project, workspace, mcp_name, creation_timestamp)
	u.lock.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func (u *UsageTracker) DeletionEvent(ctx context.Context, project string, workspace string, mcp_name string) error {
	u.lock.RLock()

	deletion_timestamp := time.Now().UTC()

	var last_usage_capture time.Time
	query := "SELECT last_usage_capture FROM mcp WHERE project = ? AND workspace = ? AND mcp = ?"
	row := u.db.QueryRowContext(ctx, query, project, workspace, mcp_name)

	u.lock.RUnlock()

	err := row.Scan(&last_usage_capture)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	u.lock.Lock()
	query = "DELETE FROM mcp WHERE project = ? AND workspace = ? AND mcp = ?"
	_, err = u.db.ExecContext(ctx, query, project, workspace, mcp_name)
	u.lock.Unlock()

	if err != nil {
		return err
	}

	// Calculate usage until deletion
	usage := deletion_timestamp.Sub(last_usage_capture).Abs()

	u.lock.Lock()
	defer u.lock.Unlock()
	err = u.trackUsage(ctx, project, workspace, mcp_name, time.Now().UTC(), usage)
	if err != nil {
		return err
	}

	return nil
}

func (u *UsageTracker) ScheduledEvent(ctx context.Context) error {
	log := u.log.WithName("scheduled")

	hourStart := time.Now().UTC().Truncate(time.Hour)

	log.Info("tracking hourly usage for mcps " + hourStart.Format(time.DateTime))

	u.lock.RLock()
	query := "SELECT project, workspace, mcp, last_usage_capture, deleted_at FROM mcp"
	rows, err := u.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	u.lock.RUnlock()
	log.Debug("done getting data from db")

	u.lock.Lock()
	defer u.lock.Unlock()
	log.Debug("start looping through results")
	for rows.Next() {
		var trackingEntry TrackingMCPEntry
		err = rows.Scan(
			&trackingEntry.Project,
			&trackingEntry.Workspace,
			&trackingEntry.Name,
			&trackingEntry.LastUsageCapture,
			&trackingEntry.DeletedAt,
		)
		log.Debug(fmt.Sprintf("entry: %v:%v:%v", trackingEntry.Project, trackingEntry.Workspace, trackingEntry.Name))
		if err != nil {
			return err
		}

		if trackingEntry.DeletedAt.Valid {
			continue
		}

		if hourStart.Compare(trackingEntry.LastUsageCapture) == -1 {
			// BillingCycleStart is in future, so no need for calculating it.
			continue
		}

		query := "UPDATE mcp SET last_usage_capture = ? WHERE project = ? AND workspace = ? AND mcp = ?"
		_, err := u.db.ExecContext(ctx, query, hourStart, trackingEntry.Project, trackingEntry.Workspace, trackingEntry.Name)
		if err != nil {
			return err
		}

		usagePerDay := calculateUsage(trackingEntry.LastUsageCapture, hourStart)
		log.Debug("Usage hours by day", "start", trackingEntry.LastUsageCapture, "end", hourStart)
		for _, usage := range usagePerDay {
			log.Debug("  usage:", "date", usage.date, "duration", usage.duration)
		}

		var errs error
		for _, usage := range usagePerDay {
			err = u.trackUsage(ctx, trackingEntry.Project, trackingEntry.Workspace, trackingEntry.Name, usage.date, usage.duration)
			if err != nil {
				errs = errors.Join(errs, err)
			}
		}
		if errs != nil {
			return errs
		}

	}

	log.Info("done tracking hourly usage " + hourStart.Format(time.DateTime))

	return nil
}

func (u *UsageTracker) trackUsage(ctx context.Context, project string, workspace string, mcp_name string, timestamp time.Time, duration time.Duration) error {
	sql := "INSERT INTO hourly_usage (project, workspace, mcp, timestamp, minutes) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO UPDATE SET minutes = EXCLUDED.minutes"
	_, err := u.db.ExecContext(ctx, sql, project, workspace, mcp_name, timestamp, duration.Minutes(), duration.Minutes())
	if err != nil {
		return err
	}

	return nil
}

func (u *UsageTracker) WriteToResource(ctx context.Context, client client.Client) error {
	log := u.log.WithName("scheduled")

	log.Info("writing usage into k8s resource")

	u.lock.RLock()
	query := `
		SELECT
			project, workspace, mcp, CAST(timestamp AS DATE) AS usage_date, SUM(minutes) AS total_daily_minutes
		FROM hourly_usage
		GROUP BY
			project, workspace, mcp, usage_date
		ORDER BY
			project, workspace, mcp, usage_date
	`
	rows, err := u.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	u.lock.RUnlock()

	var errs error
	for rows.Next() {
		var hourlyUsage HourlyUsageEntry

		err = rows.Scan(
			&hourlyUsage.Project,
			&hourlyUsage.Workspace,
			&hourlyUsage.Name,
			&hourlyUsage.Timestamp,
			&hourlyUsage.Minutes,
		)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("error when scanning hourly usage: %w", err))
			continue
		}

		chargingTarget, err := helper.ResolveChargingTarget(ctx, client, hourlyUsage.Project, hourlyUsage.Workspace, hourlyUsage.Name)
		if err != nil {
			log.Error(err, fmt.Sprintf("error when resolving charging target %v", hourlyUsage.ResourceName()))
			chargingTarget = "missing"
		}

		duration, _ := time.ParseDuration(fmt.Sprint(hourlyUsage.Minutes) + "m")
		hours := int(math.Ceil(duration.Hours()))

		resourceExistsBefore := true

		var mcpUsage v1.MCPUsage
		err = client.Get(ctx, hourlyUsage.ObjectKey(), &mcpUsage)
		if k8serrors.IsNotFound(err) {
			resourceExistsBefore = false
		} else if err != nil {
			errs = errors.Join(errs, fmt.Errorf("error at getting MCPUsage resource for %v: %w", hourlyUsage.ResourceName(), err))
			continue
		}

		mcpUsage.Name = hourlyUsage.ResourceName()
		usage, err := v1.NewDailyUsage(hourlyUsage.Timestamp, hours)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("unable to create DailyUsage entry for %v: %w", hourlyUsage.ResourceName(), err))
		}

		if !resourceExistsBefore {
			err = client.Create(ctx, &mcpUsage)
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("error at creating MCPUsage resource for %v: %w", hourlyUsage.ResourceName(), err))
				continue
			}
		}

		mcpUsage.Status.Workspace = hourlyUsage.Workspace
		mcpUsage.Status.Project = hourlyUsage.Project
		mcpUsage.Status.MCP = hourlyUsage.Name
		mcpUsage.Status.ChargingTarget = chargingTarget

		found := false
		for idx := range mcpUsage.Status.Usage {
			if mcpUsage.Status.Usage[idx].Date.Equal(&usage.Date) {
				mcpUsage.Status.Usage[idx].Usage = usage.Usage
				found = true
			}
		}
		if !found {
			mcpUsage.Status.Usage = append(mcpUsage.Status.Usage, usage)
		}

		err = client.Status().Update(ctx, &mcpUsage)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("error at updating MCPUsage status resource for %v: %w", hourlyUsage.ResourceName(), err))
			continue
		}
	}

	return errs
}
