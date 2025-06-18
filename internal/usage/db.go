package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetDB() (*sql.DB, error) {
	dbPath := os.Getenv("USAGE_DB_PATH")
	if dbPath == "" {
		fmt.Println("No DB path specified using env var USAGE_DB_PATH. Using in memory database instead.")
	}

	db, err := sql.Open("duckdb", dbPath+"?access_mode=READ_WRITE")

	return db, err
}

func InitDB(ctx context.Context, log *logging.Logger) error {
	var funcErr error

	db, err := GetDB()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			// Use errors.Join to combine the close error with any existing error
			funcErr = errors.Join(funcErr, fmt.Errorf("error closing db: %w", closeErr))
		}
	}()

	mcpTableSql := `
		CREATE TABLE IF NOT EXISTS mcp (
			project VARCHAR NOT NULL,
			workspace VARCHAR NOT NULL,
			mcp VARCHAR NOT NULL,
			last_usage_capture TIMESTAMP NOT NULL,
			deleted_at TIMESTAMP,
			PRIMARY KEY (project, workspace, mcp)
		);`

	log.Info("Creating table 'mcp' if it doesn't exist...")
	_, err = db.ExecContext(ctx, mcpTableSql)
	if err != nil {
		return err
	}

	hourlyUsageTableSQL := `
        CREATE TABLE IF NOT EXISTS hourly_usage (
            project VARCHAR NOT NULL,
            workspace VARCHAR NOT NULL,
            mcp VARCHAR NOT NULL,
            timestamp TIMESTAMP NOT NULL,
            minutes INTEGER NOT NULL,
            PRIMARY KEY (project, workspace, mcp, timestamp)
        );`

	log.Info("Creating table 'hourly_usage' if it doesn't exist...")
	_, err = db.ExecContext(ctx, hourlyUsageTableSQL)
	if err != nil {
		return err
	}

	return funcErr
}

type TrackingMCPEntry struct {
	Project          string
	Workspace        string
	Name             string
	LastUsageCapture time.Time
	DeletedAt        sql.NullTime
}

type HourlyUsageEntry struct {
	Project   string
	Workspace string
	Name      string
	Timestamp time.Time
	Minutes   int
}

func (h *HourlyUsageEntry) ResourceName() string {
	return fmt.Sprintf("%v-%v-%v", h.Project, h.Workspace, h.Name)
}

func (h *HourlyUsageEntry) ObjectKey() client.ObjectKey {
	return client.ObjectKey{
		Name: h.ResourceName(),
	}
}
