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

// TestPanelMigrationsDegradeWithoutDirectory pins OR-47: an application that
// ships no migrations directory — the default — used to get the migrator's
// error as a 500. "There is nowhere to look" is not "looking failed", and an
// empty list with no reason is indistinguishable from an application whose
// migrations are all applied, so the view says which it is.
func TestPanelMigrationsDegradeWithoutDirectory(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.MigrationsPath = filepath.Join(t.TempDir(), "no-such-directory")

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	body, status := doJSON(t, http.MethodGet, srv.URL+"/api/migrations", nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200 with no migrations directory, got %d body=%s", status, mustJSON(body))
	}
	if available, _ := body["available"].(bool); available {
		t.Errorf("expected available=false with no directory, got body=%s", mustJSON(body))
	}
	if total, _ := body["total"].(float64); total != 0 {
		t.Errorf("expected total=0, got %v", body["total"])
	}
	if message, _ := body["message"].(string); message == "" {
		t.Errorf("an empty list with no reason cannot be told from an up-to-date one: body=%s", mustJSON(body))
	}

	// Applying is refused the same way: a readable 400, not a 500 from
	// inside the migrator.
	_, applyStatus := doJSON(t, http.MethodPost, srv.URL+"/api/migrations/apply", map[string]any{})
	if applyStatus != http.StatusBadRequest {
		t.Errorf("apply with no migrations directory: got %d, want 400", applyStatus)
	}
}

// TestPanelMigrationsStillReportRealFailures is the other side of OR-47: the
// degradation must not swallow a real failure. A path that exists and is not
// a directory is a misconfiguration, and reading it as "nothing to list"
// would hide a typo in migrations_path until a deploy needed the migrations
// that were never listed.
func TestPanelMigrationsStillReportRealFailures(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	notADir := filepath.Join(t.TempDir(), "migrations")
	writeAdminTestFile(t, notADir, "this is a file, not a directory")
	panel.config.MigrationsPath = notADir

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	body, status := doJSON(t, http.MethodGet, srv.URL+"/api/migrations", nil)
	if status == http.StatusOK {
		t.Fatalf("a migrations path that is not a directory must not read as an empty list: body=%s", mustJSON(body))
	}
}
