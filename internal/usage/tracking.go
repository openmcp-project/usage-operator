package usage

import (
	"context"
	"database/sql"
	"math"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type UsageTracker struct {
	db   *sql.DB
	lock sync.RWMutex
}

func NewUsageTracker() (*UsageTracker, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	return &UsageTracker{
		db: db,
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
	_ = log.FromContext(ctx)

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
	_ = log.FromContext(ctx)

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
	_ = log.FromContext(ctx)

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
	usage := deletion_timestamp.Sub(last_usage_capture)

	minutes := int(math.Ceil(usage.Abs().Minutes()))

	err = u.trackUsage(ctx, project, workspace, mcp_name, minutes)
	if err != nil {
		return err
	}

	return nil
}

func (u *UsageTracker) ScheduledEvent(ctx context.Context) error {
	logf := log.FromContext(ctx)

	hourStart := time.Now().UTC().Truncate(time.Hour)

	logf.Info("tracking hourly usage for mcps " + hourStart.Format(time.DateTime))

	u.lock.RLock()
	query := "SELECT project, workspace, mcp, last_usage_capture, deleted_at FROM mcp"
	rows, err := u.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	u.lock.RUnlock()

	u.lock.Lock()
	defer u.lock.Unlock()
	for rows.Next() {
		var trackingEntry TrackingMCPEntry
		err = rows.Scan(
			&trackingEntry.Project,
			&trackingEntry.Workspace,
			&trackingEntry.Name,
			&trackingEntry.LastUsageCapture,
			&trackingEntry.DeletedAt,
		)
		if err != nil {
			return err
		}

		if trackingEntry.DeletedAt.Valid {
			continue
		}

		consumedDuration := hourStart.Sub(trackingEntry.LastUsageCapture)
		if consumedDuration < 0 {
			// BillingCycleStart is in future, so no need for calculating it.
			continue
		}
		consumedMinutes := int(math.Ceil(consumedDuration.Minutes()))

		query := "UPDATE mcp SET last_usage_capture = ? WHERE project = ? AND workspace = ? AND mcp = ?"
		_, err := u.db.ExecContext(ctx, query, hourStart, trackingEntry.Project, trackingEntry.Workspace, trackingEntry.Name)
		if err != nil {
			return err
		}

		query = "INSERT INTO hourly_usage (project, workspace, mcp, timestamp, minutes) VALUES (?, ?, ?, ?, ?)"
		_, err = u.db.ExecContext(ctx, query, trackingEntry.Project, trackingEntry.Workspace, trackingEntry.Name, hourStart, consumedMinutes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (u *UsageTracker) trackUsage(ctx context.Context, project string, workspace string, mcp_name string, minutes int) error {
	now := time.Now().UTC()

	u.lock.Lock()
	sql := "INSERT INTO hourly_usage (project, workspace, mcp, timestamp, minutes) VALUES (?, ?, ?, ?, ?)"
	_, err := u.db.ExecContext(ctx, sql, project, workspace, mcp_name, now, minutes)
	u.lock.Unlock()
	if err != nil {
		return err
	}

	return nil
}
