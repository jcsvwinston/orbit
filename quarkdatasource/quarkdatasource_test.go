package quarkdatasource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jcsvwinston/quark"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/orbit/datasource"
)

// QDWidget is a plain Quark model (single int64 PK).
type QDWidget struct {
	ID     int64  `db:"id" pk:"true"`
	Name   string `db:"name" quark:"not_null"`
	Qty    int64  `db:"qty"`
	Active bool   `db:"active"`
}

// QDMembership has a composite PK: listed read-only.
type QDMembership struct {
	AccountID int64 `db:"account_id" pk:"true"`
	ProjectID int64 `db:"project_id" pk:"true"`
}

// QDOrphan is registered but its table is never migrated.
type QDOrphan struct {
	ID   int64  `db:"id" pk:"true"`
	Name string `db:"name"`
}

func setup(t *testing.T) (*Adapter, context.Context) {
	t.Helper()
	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	if err := client.RegisterModel(&QDWidget{}, &QDMembership{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	if err := client.MigrateRegistered(ctx); err != nil {
		t.Fatalf("MigrateRegistered: %v", err)
	}

	a := New(client)
	for _, reg := range []func(*Adapter) error{
		Register[QDWidget], Register[QDMembership], Register[QDOrphan],
	} {
		if err := reg(a); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	return a, ctx
}

func TestCatalogue_ModelInfo(t *testing.T) {
	a, _ := setup(t)

	if got := len(a.All()); got != 3 {
		t.Fatalf("All() = %d models, want 3", got)
	}
	mi, ok := a.Get("QDWidget")
	if !ok {
		t.Fatal("QDWidget not found")
	}
	if mi.Table != "q_d_widgets" && mi.Table != "qdwidgets" {
		t.Logf("table name derived as %q", mi.Table)
	}
	if mi.PrimaryKey != "ID" {
		t.Errorf("PrimaryKey = %q, want ID", mi.PrimaryKey)
	}
	if mi.ReadOnly {
		t.Error("single-PK model must not be read-only")
	}
	f, ok := mi.Field("name")
	if !ok {
		t.Fatal("Field(name) not found by column")
	}
	if !f.IsRequired {
		t.Error("quark not_null must map to IsRequired")
	}
	if f.GoType != "string" || !f.IsSearch || !f.IsFilter {
		t.Errorf("string field defaults wrong: %+v", f)
	}
	if af, _ := mi.Field("active"); af.GoType != "bool" {
		t.Errorf("bool GoType = %q, want bool (filter normalization keys on it)", af.GoType)
	}

	// Composite PK: read-only.
	mem, _ := a.Get("QDMembership")
	if !mem.ReadOnly {
		t.Error("composite-PK model must be read-only")
	}
}

func TestCRUD_Roundtrip(t *testing.T) {
	a, ctx := setup(t)
	st, err := a.Store("QDWidget", "ignored-alias")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	created, err := st.Create(ctx, datasource.Record{"name": "alpha", "qty": 5, "active": true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	idNum, ok := created["ID"].(float64)
	if !ok || idNum <= 0 {
		t.Fatalf("created record has no assigned ID: %#v", created)
	}
	id := jsonNumberString(idNum)

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["Name"] != "alpha" || got["Qty"].(float64) != 5 {
		t.Errorf("Get = %v", got)
	}

	// Update writes zero values too (UpdateMap path).
	if err := st.Update(ctx, id, datasource.Record{"qty": 0, "name": "beta"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = st.Get(ctx, id)
	if got["Name"] != "beta" || got["Qty"].(float64) != 0 {
		t.Errorf("after Update: %v", got)
	}

	if err := st.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	page, err := st.List(ctx, datasource.Query{})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("items after delete = %d, want 0", len(page.Items))
	}
}

func TestList_FiltersSearchOrderPagination(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")

	seed := []datasource.Record{
		{"name": "red hammer", "qty": 1, "active": true},
		{"name": "blue hammer", "qty": 2, "active": true},
		{"name": "red screwdriver", "qty": 3, "active": false},
	}
	for _, r := range seed {
		if _, err := st.Create(ctx, r); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Exact filter, string value coerced to bool column.
	page, err := st.List(ctx, datasource.Query{Filters: map[string]string{"active": "1"}})
	if err != nil {
		t.Fatalf("List(filter): %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Errorf("filter active=1: total=%d items=%d, want 2/2", page.Total, len(page.Items))
	}

	// Search ANDs with filters (precedence): active=false AND name LIKE %red%.
	page, err = st.List(ctx, datasource.Query{
		Search:  "red",
		Filters: map[string]string{"active": "0"},
	})
	if err != nil {
		t.Fatalf("List(search+filter): %v", err)
	}
	if page.Total != 1 || page.Items[0]["Name"] != "red screwdriver" {
		t.Errorf("search+filter: total=%d items=%v", page.Total, page.Items)
	}

	// Order + pagination + envelope metadata.
	page, err = st.List(ctx, datasource.Query{OrderBy: "qty desc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("List(order+page): %v", err)
	}
	if len(page.Items) != 2 || page.Items[0]["Qty"].(float64) != 3 {
		t.Errorf("order desc page 1: %v", page.Items)
	}
	if page.Total != 3 || page.TotalPages != 2 || !page.HasMore || page.IsEstimated {
		t.Errorf("envelope: %+v", page)
	}
}

func TestPageEnvelope_JSONKeys(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")
	page, err := st.List(ctx, datasource.Query{})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(page)
	var got map[string]json.RawMessage
	_ = json.Unmarshal(blob, &got)
	for _, k := range []string{"items", "total", "page", "page_size", "total_pages", "is_estimated", "has_more"} {
		if _, ok := got[k]; !ok {
			t.Errorf("Page JSON missing key %q", k)
		}
	}
}

func TestCompositePK_ReadOnlyErrors(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDMembership", "")

	if _, err := st.Get(ctx, "1"); err == nil {
		t.Error("Get on composite-PK model should error")
	}
	if _, err := st.Create(ctx, datasource.Record{"account_id": 1, "project_id": 2}); err == nil {
		t.Error("Create on composite-PK model should error")
	}
	if err := st.Update(ctx, "1", datasource.Record{}); err == nil {
		t.Error("Update on composite-PK model should error")
	}
	if err := st.Delete(ctx, "1"); err == nil {
		t.Error("Delete on composite-PK model should error")
	}
	// Listing still works.
	if _, err := st.List(ctx, datasource.Query{}); err != nil {
		t.Errorf("List on composite-PK model: %v", err)
	}
}

func TestMissingTable_CountAndExists(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDOrphan", "")

	if st.TableExists(ctx) {
		t.Error("TableExists = true for a never-migrated table")
	}
	cr, err := st.Count(ctx)
	if err != nil {
		t.Fatalf("Count(missing): %v", err)
	}
	if cr.Present {
		t.Error("Count.Present = true for a missing table")
	}

	ok, _ := a.Store("QDWidget", "")
	if !ok.TableExists(ctx) {
		t.Error("TableExists = false for a migrated table")
	}
}

func TestInvalidID(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")
	if _, err := st.Get(ctx, "not-a-number"); err == nil {
		t.Error("Get with a non-numeric id must error (D1 narrowing)")
	}
}

func jsonNumberString(f float64) string {
	b, _ := json.Marshal(int64(f))
	return string(b)
}
