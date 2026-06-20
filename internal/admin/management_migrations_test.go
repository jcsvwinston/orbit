package admin

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

func TestPanelMigrationsListAndApplyUseRuntime(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	migrationsDir := t.TempDir()
	panel.config.MigrationsPath = migrationsDir

	writeAdminTestFile(t, filepath.Join(migrationsDir, "20260416090000_create_reports.up.sql"), `
CREATE TABLE reports (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL
);
`)
	writeAdminTestFile(t, filepath.Join(migrationsDir, "20260416090000_create_reports.down.sql"), `
DROP TABLE IF EXISTS reports;
`)

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	before, status := doJSON(t, http.MethodGet, srv.URL+"/api/migrations", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from migration list, got %d body=%s", status, mustJSON(before))
	}
	if got := before["mode"]; got != "runtime" {
		t.Fatalf("expected migration mode runtime, got %#v", got)
	}

	rows, ok := before["migrations"].([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one migration row, got %#v", before["migrations"])
	}
	beforeRow := rows[0].(map[string]interface{})
	if beforeRow["applied"] != false {
		t.Fatalf("expected migration to be pending before apply, got %#v", beforeRow["applied"])
	}

	applied, status := doJSON(t, http.MethodPost, srv.URL+"/api/migrations/apply", map[string]any{"steps": 0})
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from migration apply, got %d body=%s", status, mustJSON(applied))
	}
	if got := int(applied["applied"].(float64)); got != 1 {
		t.Fatalf("expected applied=1, got %d", got)
	}
	if got := int(applied["pending"].(float64)); got != 0 {
		t.Fatalf("expected pending=0, got %d", got)
	}

	after, status := doJSON(t, http.MethodGet, srv.URL+"/api/migrations", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from migration list after apply, got %d body=%s", status, mustJSON(after))
	}
	afterRows := after["migrations"].([]interface{})
	afterRow := afterRows[0].(map[string]interface{})
	if afterRow["applied"] != true {
		t.Fatalf("expected migration to be applied after execute, got %#v", afterRow["applied"])
	}
	if _, ok := afterRow["applied_at"].(string); !ok {
		t.Fatalf("expected applied_at in migration response, got %#v", afterRow["applied_at"])
	}

	sqlDB, err := panel.db.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}
	if !adminTableExists(t, sqlDB, "reports") {
		t.Fatal("expected reports table to exist after applying migrations")
	}
}

func TestPanelMigrationsApplyRejectsInvalidJSON(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.MigrationsPath = t.TempDir()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/migrations/apply", bytes.NewBufferString("{"))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status 400, got %d body=%s", res.StatusCode, string(raw))
	}
}

func writeAdminTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write file %s failed: %v", path, err)
	}
}

func adminTableExists(t *testing.T, sqlDB *sql.DB, table string) bool {
	t.Helper()

	var cnt int
	row := sqlDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table)
	if err := row.Scan(&cnt); err != nil {
		t.Fatalf("tableExists scan failed: %v", err)
	}
	return cnt > 0
}
