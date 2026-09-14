package admin

// Tests for A6 S3: the audit trail stops being a process-lifetime buffer.
//
// What these hold down is the part a status code cannot show — that a second
// panel on the same database reads what the first one recorded, that a
// retention window actually DELETES what falls outside it, that the export
// carries the entries and not a header, and that the record history obeys the
// permissions of the record it belongs to.

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	dsnucleus "github.com/jcsvwinston/orbit/internal/datasource/nucleus"
)

// auditPanel builds a panel whose trail is kept in the database at dbPath —
// a file, not :memory:, because the question is what a SECOND process sees.
func auditPanel(t *testing.T, dbPath string, tune func(*PanelConfig)) (*Panel, *sql.DB, *httptest.Server) {
	t.Helper()

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine: db.EngineSQL, DatabaseURL: "sqlite://" + dbPath, DatabaseMaxOpen: 1, DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	if err := ensureAdminUserSchema(sqlDB); err != nil {
		t.Fatalf("admin schema: %v", err)
	}
	if _, err := sqlDB.Exec(ownedNotesDDL); err != nil {
		t.Fatalf("owned_notes schema: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &OwnedNote{}} {
		if err := registry.Register(m); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	var panel *Panel
	src := dsnucleus.New(dsnucleus.Config{
		Registry: registry,
		Resolve: func(alias string) (*db.DB, string, error) {
			h, err := panel.databaseHandle(alias)
			if err != nil {
				return nil, "", err
			}
			return h, h.System(), nil
		},
		BusConnected: func() bool { return true },
	})
	cfg := PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            operatorAuth(),
		SchemaRegistry:  registry,
		DatabaseHandles: map[string]*db.DB{"default": database},
		AuditEnabled:    true,
		AuditMaxSize:    100,
		RowOwnerFields:  map[string]string{"OwnedNote": "owner"},
	}
	if tune != nil {
		tune(&cfg)
	}
	panel = NewPanel(src, logger, cfg)

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

func auditDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "audit.db")
}

// The whole point: a second panel on the same database reads what the first
// one recorded. Nothing else answers what an incident asks.
func TestAuditSQL_TrailSurvivesTheProcess(t *testing.T) {
	path := auditDBPath(t)

	first, _, firstSrv := auditPanel(t, path, nil)
	if !first.audit.persistent() {
		t.Fatal("a panel with a database handle kept its trail in memory")
	}
	resp, status := doJSON(t, http.MethodPost, firstSrv.URL+"/api/models/OwnedNote",
		map[string]any{"title": "written before the restart"})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}

	// A second panel, as a redeploy would build it.
	_, _, secondSrv := auditPanel(t, path, nil)
	trail, status := doJSON(t, http.MethodGet, secondSrv.URL+"/api/audit?page_size=200", nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(trail))
	}
	body := mustJSON(trail)
	if !strings.Contains(body, "written before the restart") {
		t.Fatalf("the new process sees none of the old trail: %s", body)
	}
	if persistent, _ := trail["persistent"].(bool); !persistent {
		t.Error("the payload does not say the trail is persistent")
	}
}

// The ring is still there for a panel that asks for it, and it still says so.
func TestAuditSQL_MemoryStoreIsStillAvailable(t *testing.T) {
	panel, _, srv := auditPanel(t, auditDBPath(t), func(cfg *PanelConfig) {
		cfg.AuditStore = auditStoreMemory
	})
	if panel.audit.persistent() {
		t.Fatal("audit_store: memory kept the trail in the database")
	}
	trail, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit", nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(trail))
	}
	if persistent, _ := trail["persistent"].(bool); persistent {
		t.Error("the memory store reports itself as persistent")
	}
}

// A panel with no database handle keeps working, with the ring it always had.
func TestAuditSQL_NoDatabaseHandleFallsBackToTheRing(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	registry := model.NewRegistry()
	if err := registry.Register(&AdminUser{}); err != nil {
		t.Fatal(err)
	}
	panel := NewPanel(dsnucleus.New(dsnucleus.Config{Registry: registry}), logger, PanelConfig{
		Prefix: "/admin", Auth: operatorAuth(), AuditEnabled: true, AuditMaxSize: 10,
	})
	if panel.audit == nil {
		t.Fatal("audit enabled but no store at all")
	}
	if panel.audit.persistent() {
		t.Fatal("a panel with no database handle claims a persistent trail")
	}
	panel.audit.add(AuditEntry{Action: "login", Username: "root"})
	if got := panel.audit.count(auditQueryOpts{}); got != 1 {
		t.Fatalf("the fallback ring holds %d entries, want 1", got)
	}
}

// insertStaleEntry writes one row dated age ago, which no API can do: a
// retention window cannot be measured without an entry outside it.
func insertStaleEntry(t *testing.T, sqlDB *sql.DB, recordID string, age time.Duration) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO nucleus_admin_audit (user_id, username, action, model_name, record_id, ip, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"u", "u", "create", "OwnedNote", recordID, "127.0.0.1", "test",
		time.Now().UTC().Add(-age).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert stale entry: %v", err)
	}
}

// Retention is a window that is APPLIED, not a number that is stored.
func TestAuditSQL_RetentionDropsWhatFallsOutsideTheWindow(t *testing.T) {
	_, sqlDB, srv := auditPanel(t, auditDBPath(t), nil)

	insertStaleEntry(t, sqlDB, "old-row", 400*24*time.Hour)
	insertStaleEntry(t, sqlDB, "recent-row", 24*time.Hour)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/audit/retention", map[string]any{"retention_days": 30})
	if status != http.StatusOK {
		t.Fatalf("set retention: status %d body=%s", status, mustJSON(resp))
	}
	if dropped, _ := resp["dropped"].(float64); dropped != 1 {
		t.Errorf("dropped = %v, want exactly the entry outside the window", resp["dropped"])
	}

	trail, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit?page_size=200", nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(trail))
	}
	body := mustJSON(trail)
	if strings.Contains(body, "old-row") {
		t.Error("an entry older than the window survived it")
	}
	if !strings.Contains(body, "recent-row") {
		t.Error("the window dropped an entry inside it")
	}

	policy, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit/retention", nil)
	if status != http.StatusOK {
		t.Fatalf("retention: status %d body=%s", status, mustJSON(policy))
	}
	if days, _ := policy["retention_days"].(float64); days != 30 {
		t.Errorf("retention_days = %v after the change, want 30", policy["retention_days"])
	}
	// What a restart comes back to is the configured value, and the payload
	// says so rather than letting an operator believe the change is durable.
	if days, _ := policy["configured_retention_days"].(float64); days != 0 {
		t.Errorf("configured_retention_days = %v, want the application's own value", policy["configured_retention_days"])
	}
}

// The window configured by the application is applied at mount, not on the
// next write: a deploy that lowered it should not wait for traffic.
func TestAuditSQL_ConfiguredRetentionIsAppliedAtMount(t *testing.T) {
	path := auditDBPath(t)
	_, sqlDB, _ := auditPanel(t, path, nil)
	insertStaleEntry(t, sqlDB, "ancient", 400*24*time.Hour)

	_, _, secondSrv := auditPanel(t, path, func(cfg *PanelConfig) { cfg.AuditRetentionDays = 7 })
	trail, status := doJSON(t, http.MethodGet, secondSrv.URL+"/api/audit?page_size=200", nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(trail))
	}
	if strings.Contains(mustJSON(trail), "ancient") {
		t.Fatal("the configured window was not applied when the panel came up")
	}
}

func TestAuditSQL_RetentionRefusesNonsense(t *testing.T) {
	_, _, srv := auditPanel(t, auditDBPath(t), nil)
	for _, body := range []map[string]any{
		{"retention_days": -1},
		{"retention_days": maxAuditRetentionDays + 1},
		{},
	} {
		resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/audit/retention", body)
		if status != http.StatusBadRequest {
			t.Errorf("%v: status %d body=%s, want 400", body, status, mustJSON(resp))
		}
	}
}

// The export is a FILE with the entries in it, which is what a compliance
// request asks for — and taking a copy of the log is itself recorded.
func TestAuditSQL_ExportCarriesTheEntriesAndIsAudited(t *testing.T) {
	panel, _, srv := auditPanel(t, auditDBPath(t), nil)

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote", map[string]any{"title": "exported row"})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}

	body := getText(t, srv.URL+"/api/audit?format=csv")
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v: %s", err, body)
	}
	if len(rows) < 2 {
		t.Fatalf("the export carries a header and no entries: %s", body)
	}
	if rows[0][0] != "id" || rows[0][1] != "created_at" {
		t.Errorf("unexpected header: %v", rows[0])
	}
	if !strings.Contains(body, "exported row") {
		t.Errorf("the export does not carry the values of the entry: %s", body)
	}
	if got := panel.audit.count(auditQueryOpts{Action: "audit.export"}); got != 1 {
		t.Errorf("taking a copy of the trail left %d entries, want 1", got)
	}
}

// The export honours the filters the listing was looking at: what you export
// is what you were reading.
func TestAuditSQL_ExportHonoursFilters(t *testing.T) {
	_, _, srv := auditPanel(t, auditDBPath(t), nil)
	for i := 0; i < 3; i++ {
		if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
			map[string]any{"title": fmt.Sprintf("row %d", i)}); status != http.StatusCreated {
			t.Fatalf("create %d: status %d", i, status)
		}
	}
	if _, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1",
		map[string]any{"title": "edited row"}); status != http.StatusOK {
		t.Fatal("update failed")
	}

	body := getText(t, srv.URL+"/api/audit?format=csv&action=update")
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	for _, row := range rows[1:] {
		if row[4] != "update" {
			t.Fatalf("an entry outside the filter reached the export: %v", row)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("the filtered export carries %d entries, want 1", len(rows)-1)
	}
}

// The history of a record is the trail read by record — with the values.
func TestAuditSQL_RecordHistoryCarriesBeforeAndAfter(t *testing.T) {
	_, _, srv := auditPanel(t, auditDBPath(t), nil)

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
		map[string]any{"title": "first title"})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}
	if _, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1",
		map[string]any{"title": "second title"}); status != http.StatusOK {
		t.Fatal("update failed")
	}

	history, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/1/history", nil)
	if status != http.StatusOK {
		t.Fatalf("history: status %d body=%s", status, mustJSON(history))
	}
	body := mustJSON(history)
	if !strings.Contains(body, "first title") || !strings.Contains(body, "second title") {
		t.Fatalf("the history does not carry what the row said before and after: %s", body)
	}
	if !strings.Contains(body, `"username":"operator"`) {
		t.Errorf("the history does not name who changed the row: %s", body)
	}

	// Another record's history is its own.
	if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
		map[string]any{"title": "unrelated"}); status != http.StatusCreated {
		t.Fatal("create failed")
	}
	other, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/2/history", nil)
	if status != http.StatusOK {
		t.Fatalf("history: status %d body=%s", status, mustJSON(other))
	}
	if strings.Contains(mustJSON(other), "first title") {
		t.Errorf("the history of one record carries another's: %s", mustJSON(other))
	}
}

// The history obeys the permissions of the record it belongs to: a field the
// operator may not read is not readable through the row's own history, and a
// row they cannot open has no history for them either.
func TestAuditSQL_RecordHistoryObeysRecordPermissions(t *testing.T) {
	path := auditDBPath(t)
	panel, sqlDB, srv := auditPanel(t, path, nil)

	if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
		map[string]any{"title": "visible", "secret": "classified"}); status != http.StatusCreated {
		t.Fatal("create failed")
	}
	if _, err := sqlDB.Exec(`UPDATE owned_notes SET owner = 'somebody-else' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, pol := range [][3]string{
		{"operator", "admin:OwnedNote", "retrieve"},
		{"operator", "admin:OwnedNote.secret", "deny"},
	} {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatal(err)
		}
	}
	panel.rbac = enf

	history, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/1/history", nil)
	if status != http.StatusOK {
		t.Fatalf("history: status %d body=%s", status, mustJSON(history))
	}
	if strings.Contains(mustJSON(history), "classified") {
		t.Errorf("a field the operator may not read is readable through the history: %s", mustJSON(history))
	}

	// Now confine the same operator to their own rows: row 1 belongs to
	// somebody else, so its history is as absent as the row.
	enf2, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := enf2.AddPolicy("operator", "admin:OwnedNote#own", "retrieve"); err != nil {
		t.Fatal(err)
	}
	panel.rbac = enf2
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/1/history", nil)
	if status != http.StatusNotFound {
		t.Fatalf("history of another operator's row: status %d body=%s, want 404", status, mustJSON(resp))
	}
}

// A trail in the database is bounded by its retention window, not by
// AuditMaxSize: the ring's count bound must not silently apply to it.
func TestAuditSQL_DatabaseTrailIsNotBoundedByRingSize(t *testing.T) {
	_, _, srv := auditPanel(t, auditDBPath(t), func(cfg *PanelConfig) { cfg.AuditMaxSize = 5 })

	for i := 0; i < 12; i++ {
		if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
			map[string]any{"title": fmt.Sprintf("row %d", i)}); status != http.StatusCreated {
			t.Fatalf("create %d: status %d", i, status)
		}
	}
	trail, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit?page_size=200", nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(trail))
	}
	if total, _ := trail["total"].(float64); total < 12 {
		t.Fatalf("total = %v with a ring size of 5: the database trail was trimmed like a ring", trail["total"])
	}
}
