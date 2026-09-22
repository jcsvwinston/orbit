package datastudio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"

	"github.com/jcsvwinston/orbit/datasource"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// memSource is a DataSource over a map: one model, records keyed the way the
// Nucleus adapter keys them (JSON tags: "id", "title", "tenant_id"), so the
// tests exercise the key translation the wire needs.
type memSource struct {
	info  datasource.ModelInfo
	rows  map[string]datasource.Record
	next  int
	seen  []datasource.Query
	claim string // subject the last Create saw through auth.ClaimsFromContext
}

func newMemSource() *memSource {
	return &memSource{
		info: datasource.ModelInfo{
			Name: "Article", Plural: "Articles", Table: "articles", PrimaryKey: "id", TenantField: "TenantID",
			Fields: []datasource.FieldInfo{
				{Name: "ID", Column: "id", IsPK: true},
				{Name: "Title", Column: "title"},
				{Name: "TenantID", Column: "tenant_id", IsTenantField: true},
				{Name: "Secret", Column: "secret", IsExcluded: true},
			},
		},
		rows: map[string]datasource.Record{
			"1": {"id": 1, "title": "tenant a, first", "tenant_id": "a", "secret": "x"},
			"2": {"id": 2, "title": "tenant a, second", "tenant_id": "a", "secret": "y"},
			"3": {"id": 3, "title": "tenant b, only", "tenant_id": "b", "secret": "z"},
		},
		next: 4,
	}
}

func (m *memSource) All() []datasource.ModelInfo { return []datasource.ModelInfo{m.info} }
func (m *memSource) Get(name string) (datasource.ModelInfo, bool) {
	if strings.EqualFold(name, m.info.Name) {
		return m.info, true
	}
	return datasource.ModelInfo{}, false
}
func (m *memSource) Store(modelName, dbAlias string) (datasource.RecordStore, error) {
	if !strings.EqualFold(modelName, m.info.Name) {
		return nil, fmt.Errorf("no model %q", modelName)
	}
	return &memStore{m}, nil
}

// memStore is the RecordStore half of memSource.
type memStore struct{ *memSource }

func (m *memStore) List(ctx context.Context, q datasource.Query) (datasource.Page, error) {
	m.seen = append(m.seen, q)
	var items []datasource.Record
	for _, id := range []string{"1", "2", "3"} {
		rec := m.rows[id]
		keep := true
		for col, want := range q.Filters {
			if fmt.Sprint(rec[col]) != want {
				keep = false
			}
		}
		if keep {
			items = append(items, rec)
		}
	}
	return datasource.Page{Items: items, Total: int64(len(items)), Page: 1, PageSize: 25}, nil
}
func (m *memStore) Get(ctx context.Context, id string) (datasource.Record, error) {
	rec, ok := m.rows[id]
	if !ok {
		return nil, errors.New("not found: no such row")
	}
	return rec, nil
}
func (m *memStore) Create(ctx context.Context, rec datasource.Record) (datasource.Record, error) {
	if c, ok := auth.ClaimsFromContext(ctx); ok && c != nil {
		m.claim = c.UserID
	}
	id := fmt.Sprint(m.next)
	m.next++
	stored := datasource.Record{"id": m.next - 1}
	for k, v := range rec {
		stored[strings.ToLower(k)] = v
	}
	if t, ok := rec["TenantID"]; ok {
		stored["tenant_id"] = t
	}
	m.rows[id] = stored
	return stored, nil
}
func (m *memStore) Update(ctx context.Context, id string, rec datasource.Record) error {
	row, ok := m.rows[id]
	if !ok {
		return errors.New("not found: no such row")
	}
	for k, v := range rec {
		row[strings.ToLower(k)] = v
	}
	return nil
}
func (m *memStore) Delete(ctx context.Context, id string) error {
	if _, ok := m.rows[id]; !ok {
		return errors.New("not found: no such row")
	}
	delete(m.rows, id)
	return nil
}
func (m *memStore) Count(ctx context.Context) (datasource.CountResult, error) {
	return datasource.CountResult{Count: int64(len(m.rows)), Present: true}, nil
}
func (m *memStore) TableExists(ctx context.Context) bool { return true }

// rowsOnly is a PolicySource that exposes rows and cannot decide for itself:
// the handler must compile it into an enforcer.
type rowsOnly [][]string

func (r rowsOnly) GetPolicy() ([][]string, error)         { return r, nil }
func (r rowsOnly) GetGroupingPolicy() ([][]string, error) { return nil, nil }
func (r rowsOnly) GetAllRoles() ([]string, error)         { return nil, nil }

func listReq(op *adminv1.OperatorIdentity) *adminv1.DataStudioRequest {
	return &adminv1.DataStudioRequest{RequestId: "r", Operator: op, Body: &adminv1.DataStudioRequest_ListRecords{
		ListRecords: &adminv1.ListRecordsRequest{ModelName: "Article", Page: 1, PageSize: 25},
	}}
}

func TestDispatch_WireKeysAreFieldNames(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src})
	resp := h.Dispatch(context.Background(), listReq(nil))
	if resp.Error != "" {
		t.Fatalf("list: %s", resp.Error)
	}
	page := resp.GetRecordsPage()
	if len(page.GetItems()) != 3 || page.GetTotal() != 3 {
		t.Fatalf("want 3 rows and total 3, got %d/%d", len(page.GetItems()), page.GetTotal())
	}
	first := page.GetItems()[0].GetValuesJson()
	if first["Title"] != `"tenant a, first"` || first["TenantID"] != `"a"` || first["ID"] != "1" {
		t.Fatalf("values_json must be keyed by field NAME with JSON values (the source keys by JSON tag): %v", first)
	}
	if _, leaked := first["Secret"]; leaked {
		t.Fatal("an excluded field reached the wire")
	}
	if !src.seen[0].ExactTotal {
		t.Fatal("every list asks for an exact total")
	}
}

func TestDispatch_NoOperator_NoPolicyNoTenant(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src, Authorizer: rowsOnly{{"*", "admin:Article", "*", "deny"}}})
	resp := h.Dispatch(context.Background(), listReq(nil))
	if resp.Error != "" {
		t.Fatalf("a request without an operator keeps the pre-ADR-002 behaviour (no policy applied), got %s", resp.Error)
	}
	if n := len(resp.GetRecordsPage().GetItems()); n != 3 {
		t.Fatalf("no operator, no tenant scope: want every row, got %d", n)
	}
}

func TestDispatch_PolicyDeniesTheOperator(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src, Authorizer: rowsOnly{{"*", "admin:Article", "*", "deny"}}})
	resp := h.Dispatch(context.Background(), listReq(&adminv1.OperatorIdentity{Subject: "bench-operator", Role: "ui-operator"}))
	if !strings.HasPrefix(resp.Error, "permission denied:") {
		t.Fatalf("a denied operator must be refused with the permission-denied prefix, got %q", resp.Error)
	}
	if len(src.seen) != 0 {
		t.Fatal("the store was consulted although the policy refused")
	}
}

func TestDispatch_PolicyAllowsBySubjectOrRole(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src, Authorizer: rowsOnly{{"editors", "admin:Article", "list", "allow"}}})
	if resp := h.Dispatch(context.Background(), listReq(&adminv1.OperatorIdentity{Subject: "alice", Role: "editors"})); resp.Error != "" {
		t.Fatalf("a role granted list must pass: %s", resp.Error)
	}
	if resp := h.Dispatch(context.Background(), listReq(&adminv1.OperatorIdentity{Subject: "alice", Role: "viewers"})); !strings.HasPrefix(resp.Error, "permission denied:") {
		t.Fatalf("a role not granted list must be refused, got %q", resp.Error)
	}
}

func TestDispatch_TenantScopeConfinesReadsAndWrites(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src})
	scoped := &adminv1.OperatorIdentity{Subject: "bench-operator", Tenant: "a"}

	// Reads: the scope is an equality filter on the tenant column.
	resp := h.Dispatch(context.Background(), listReq(scoped))
	if resp.Error != "" {
		t.Fatalf("list: %s", resp.Error)
	}
	if n := len(resp.GetRecordsPage().GetItems()); n != 2 {
		t.Fatalf("tenant a sees 2 rows, got %d", n)
	}
	if src.seen[0].Filters["tenant_id"] != "a" {
		t.Fatalf("the scope must be a filter on the tenant COLUMN, got %v", src.seen[0].Filters)
	}

	// A row of another tenant is not found, not forbidden.
	get := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_GetRecord{
		GetRecord: &adminv1.GetRecordRequest{ModelName: "Article", Id: "3"}}})
	if !strings.HasPrefix(get.Error, "not found:") {
		t.Fatalf("tenant b's row must be not found for tenant a, got %q", get.Error)
	}

	// A create is stamped with the tenant; one naming another tenant is refused.
	create := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_CreateRecord{
		CreateRecord: &adminv1.CreateRecordRequest{ModelName: "Article", Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"new"`}}}}})
	if create.Error != "" {
		t.Fatalf("create: %s", create.Error)
	}
	if got := create.GetRecord().GetValuesJson()["TenantID"]; got != `"a"` {
		t.Fatalf("the created row must carry the operator's tenant, got %s", got)
	}
	if src.claim != "bench-operator" {
		t.Fatalf("the framework identity must reach the store's context (auth.ClaimsFromContext), saw %q", src.claim)
	}
	moved := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_CreateRecord{
		CreateRecord: &adminv1.CreateRecordRequest{ModelName: "Article", Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"x"`, "TenantID": `"b"`}}}}})
	if !strings.HasPrefix(moved.Error, "permission denied:") {
		t.Fatalf("a create naming another tenant must be refused, got %q", moved.Error)
	}

	// Update and delete confirm ownership first.
	upd := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_UpdateRecord{
		UpdateRecord: &adminv1.UpdateRecordRequest{ModelName: "Article", Id: "3", Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"stolen"`}}}}})
	if !strings.HasPrefix(upd.Error, "not found:") {
		t.Fatalf("updating tenant b's row as tenant a must be not found, got %q", upd.Error)
	}
	del := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordRequest{ModelName: "Article", Id: "3"}}})
	if !strings.HasPrefix(del.Error, "not found:") {
		t.Fatalf("deleting tenant b's row as tenant a must be not found, got %q", del.Error)
	}
	if _, still := src.rows["3"]; !still {
		t.Fatal("tenant b's row was deleted through tenant a's scope")
	}
	bulk := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: scoped, Body: &adminv1.DataStudioRequest_BulkAction{
		BulkAction: &adminv1.BulkActionRequest{ModelName: "Article", Action: "delete", Ids: []string{"2", "3"}}}})
	if b := bulk.GetBulkAction(); b.GetAffected() != 1 || b.GetFailed() != 1 {
		t.Fatalf("bulk delete across the scope: want 1 affected and 1 failed, got %d/%d (%v)", b.GetAffected(), b.GetFailed(), b.GetErrors())
	}
}

func TestDispatch_ReadOnlyOperatorCannotWrite(t *testing.T) {
	src := newMemSource()
	h := New(Config{Source: src, Authorizer: rowsOnly{{"viewer", "admin:Article", "list", "allow"}, {"viewer", "admin:Article", "delete", "allow"}}})
	ro := &adminv1.OperatorIdentity{Subject: "viewer", ReadOnly: true}
	if resp := h.Dispatch(context.Background(), listReq(ro)); resp.Error != "" {
		t.Fatalf("a read-only operator may list: %s", resp.Error)
	}
	del := h.Dispatch(context.Background(), &adminv1.DataStudioRequest{Operator: ro, Body: &adminv1.DataStudioRequest_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordRequest{ModelName: "Article", Id: "1"}}})
	if !strings.HasPrefix(del.Error, "permission denied:") {
		t.Fatalf("a read-only operator may not delete, got %q", del.Error)
	}
}
