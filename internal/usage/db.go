package usage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/marcboeker/go-duckdb"
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

func InitDB(ctx context.Context) error {
	db, err := GetDB()
	if err != nil {
		return err
	}
	defer db.Close()

	mcpTableSql := `
		CREATE TABLE IF NOT EXISTS mcp (
			project VARCHAR NOT NULL,
			workspace VARCHAR NOT NULL,
			mcp VARCHAR NOT NULL,
			last_usage_capture TIMESTAMPTZ NOT NULL,
			deleted_at TIMESTAMPTZ,
			PRIMARY KEY (project, workspace, mcp)
		);`
	log.Println("Creating table 'mcp' if it doesn't exist...")
	_, err = db.ExecContext(ctx, mcpTableSql)
	if err != nil {
		return err
	}

	hourlyUsageTableSQL := `
        CREATE TABLE IF NOT EXISTS hourly_usage (
            project VARCHAR NOT NULL,
            workspace VARCHAR NOT NULL,
            mcp VARCHAR NOT NULL,
            timestamp TIMESTAMPTZ NOT NULL,
            minutes INTEGER NOT NULL,
            PRIMARY KEY (project, workspace, mcp, timestamp)
        );`

	log.Println("Creating table 'hourly_usage' if it doesn't exist...")
	_, err = db.ExecContext(ctx, hourlyUsageTableSQL)
	if err != nil {
		return err
	}

	return nil
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
	return fmt.Sprintf("%v:%v:%v", h.Project, h.Workspace, h.Name)
}

func (h *HourlyUsageEntry) ObjectKey() client.ObjectKey {
	return client.ObjectKey{
		Name: h.ResourceName(),
	}
}
