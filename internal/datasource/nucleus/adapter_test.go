package nucleus

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/internal/datasource"
)

// DSWidget is a model with a known table so the test can seed rows directly.
type DSWidget struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Qty  int    `json:"qty"`
}

func (DSWidget) TableName() string { return "ds_widgets" }

// DSGadget is registered but its table is never created, to exercise the
// missing-table paths (TableExists false, Count not present).
type DSGadget struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func (DSGadget) TableName() string { return "ds_gadgets" }

func setupAdapter(t *testing.T) *Adapter {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE ds_widgets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		qty INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	reg := model.NewRegistry()
	if err := reg.Register(&DSWidget{}); err != nil {
		t.Fatalf("register DSWidget: %v", err)
	}
	if err := reg.Register(&DSGadget{}); err != nil {
		t.Fatalf("register DSGadget: %v", err)
	}

	return New(Config{
		Registry:     reg,
		DefaultAlias: "default",
		Resolve: func(string) (*db.DB, string, error) {
			return database, "sqlite", nil
		},
	})
}

func TestModelInfo_Mapping(t *testing.T) {
	a := setupAdapter(t)
	mi, ok := a.Get("DSWidget")
	if !ok {
		t.Fatal("DSWidget not found")
	}
	if mi.Table != "ds_widgets" {
		t.Errorf("Table = %q, want ds_widgets", mi.Table)
	}
	if mi.PrimaryKey != "ID" {
		t.Errorf("PrimaryKey = %q, want ID", mi.PrimaryKey)
	}
	// Neutral field lookup works by column and by Go name.
	if _, ok := mi.Field("name"); !ok {
		t.Error("Field(\"name\") not found by column")
	}
	if _, ok := mi.Field("Qty"); !ok {
		t.Error("Field(\"Qty\") not found by Go name")
	}
	if len(a.All()) != 2 {
		t.Errorf("All() = %d models, want 2", len(a.All()))
	}
}

func TestStore_CRUD_Roundtrip(t *testing.T) {
	a := setupAdapter(t)
	ctx := context.Background()
	st, err := a.Store("DSWidget", "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	created, err := st.Create(ctx, datasource.Record{"name": "alpha", "qty": 5})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := recordID(t, created)
	if created["name"] != "alpha" {
		t.Errorf("created name = %v, want alpha", created["name"])
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["name"] != "alpha" || asInt(got["qty"]) != 5 {
		t.Errorf("Get = %v, want name=alpha qty=5", got)
	}

	if err := st.Update(ctx, id, datasource.Record{"qty": 9}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = st.Get(ctx, id)
	if asInt(got["qty"]) != 9 {
		t.Errorf("after Update qty = %v, want 9", got["qty"])
	}

	page, err := st.List(ctx, datasource.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("List items = %d, want 1", len(page.Items))
	}

	if err := st.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	page, _ = st.List(ctx, datasource.Query{})
	if len(page.Items) != 0 {
		t.Errorf("after Delete items = %d, want 0", len(page.Items))
	}
}

// The Page JSON envelope must carry exactly the keys the embedded SPA reads
// (ADR-001 O3), matching Nucleus's native model.PaginatedResult.
func TestPage_EnvelopeKeys(t *testing.T) {
	blob, err := json.Marshal(datasource.Page{Items: []datasource.Record{}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"items", "total", "page", "page_size", "total_pages", "is_estimated", "has_more"}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("Page JSON missing key %q", k)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Page JSON has %d keys, want %d (%v)", len(got), len(want), keysOf(got))
	}

	// Cross-check against the native envelope the panel used to forward.
	nativeBlob, _ := json.Marshal(model.PaginatedResult{})
	var native map[string]json.RawMessage
	_ = json.Unmarshal(nativeBlob, &native)
	for k := range native {
		if _, ok := got[k]; !ok {
			t.Errorf("Page JSON missing native key %q", k)
		}
	}
}

// entityToRecord must reproduce the exact JSON the struct would emit, so records
// forwarded as maps look identical to the old struct forwarding (O3).
func TestEntityToRecord_Fidelity(t *testing.T) {
	w := DSWidget{ID: 7, Name: "gizmo", Qty: 3}
	rec, err := entityToRecord(w)
	if err != nil {
		t.Fatal(err)
	}
	structJSON, _ := json.Marshal(w)
	recJSON, _ := json.Marshal(rec)
	if string(structJSON) != string(recJSON) {
		t.Errorf("record JSON diverges from struct JSON:\n struct: %s\n record: %s", structJSON, recJSON)
	}
}

func TestCount_And_TableExists(t *testing.T) {
	a := setupAdapter(t)
	ctx := context.Background()
	st, _ := a.Store("DSWidget", "")
	if _, err := st.Create(ctx, datasource.Record{"name": "a", "qty": 1}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !st.TableExists(ctx) {
		t.Error("TableExists = false for an existing table")
	}
	cr, err := st.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if !cr.Present {
		t.Error("Count.Present = false for an existing table")
	}

	// A model whose table was never created.
	miss, _ := a.Store("DSGadget", "")
	if miss.TableExists(ctx) {
		t.Error("TableExists = true for a missing table")
	}
	cr, err = miss.Count(ctx)
	if err != nil {
		t.Fatalf("Count(missing): %v", err)
	}
	if cr.Present {
		t.Error("Count.Present = true for a missing table")
	}
}

func TestGet_InvalidID(t *testing.T) {
	a := setupAdapter(t)
	st, _ := a.Store("DSWidget", "")
	if _, err := st.Get(context.Background(), "not-a-number"); err == nil {
		t.Error("Get with non-numeric id should error (D1 narrowing)")
	}
}

// helpers

func recordID(t *testing.T, rec datasource.Record) string {
	t.Helper()
	switch v := rec["id"].(type) {
	case float64:
		return itoa(int64(v))
	case json.Number:
		return v.String()
	default:
		t.Fatalf("record has no numeric id: %#v", rec["id"])
		return ""
	}
}

func asInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return -1
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
