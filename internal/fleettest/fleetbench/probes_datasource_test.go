// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/jcsvwinston/nucleus/pkg/db"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
)

// ---- protocol descriptor ------------------------------------------------------------
//
// The wire contract is the generated descriptor of admin.proto. Reading it
// is a measurement of what the protocol CAN carry: a field the descriptor
// never declares cannot cross the stream, whatever the code around it does.

func allMessages() []protoreflect.MessageDescriptor {
	var out []protoreflect.MessageDescriptor
	var walk func(ms protoreflect.MessageDescriptors)
	walk = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			m := ms.Get(i)
			out = append(out, m)
			walk(m.Messages())
		}
	}
	walk(adminv1.File_nucleus_admin_v1_admin_proto.Messages())
	return out
}

func messageNamed(t *testing.T, name string) protoreflect.MessageDescriptor {
	t.Helper()
	for _, m := range allMessages() {
		if string(m.Name()) == name {
			return m
		}
	}
	t.Fatalf("admin.proto has no message %q", name)
	return nil
}

func containsAny(name string, substrs []string) bool {
	lower := strings.ToLower(name)
	for _, s := range substrs {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// fieldsContaining lists the fields of md whose name contains any substring.
func fieldsContaining(md protoreflect.MessageDescriptor, substrs ...string) []string {
	var out []string
	fs := md.Fields()
	for i := 0; i < fs.Len(); i++ {
		if name := string(fs.Get(i).Name()); containsAny(name, substrs) {
			out = append(out, name)
		}
	}
	return out
}

// anyFieldContaining lists "Message.field" for every field in the file
// whose name contains any substring.
func anyFieldContaining(substrs ...string) []string {
	var out []string
	for _, m := range allMessages() {
		for _, f := range fieldsContaining(m, substrs...) {
			out = append(out, string(m.Name())+"."+f)
		}
	}
	return out
}

func messagesContaining(substrs ...string) []string {
	var out []string
	for _, m := range allMessages() {
		if containsAny(string(m.Name()), substrs) {
			out = append(out, string(m.Name()))
		}
	}
	return out
}

// methodsContaining lists "Service.Method" for every RPC whose name
// contains any substring.
func methodsContaining(substrs ...string) []string {
	var out []string
	svcs := adminv1.File_nucleus_admin_v1_admin_proto.Services()
	for i := 0; i < svcs.Len(); i++ {
		s := svcs.Get(i)
		ms := s.Methods()
		for j := 0; j < ms.Len(); j++ {
			if name := string(ms.Get(j).Name()); containsAny(name, substrs) {
				out = append(out, string(s.Name())+"."+name)
			}
		}
	}
	return out
}

// ---- helpers -------------------------------------------------------------------------------

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// listArticles asks the fleet for TestArticle rows with the given filters.
func listArticles(ctx context.Context, ds interface {
	ListRecords(context.Context, *connect.Request[adminv1.ListRecordsRequest]) (*connect.Response[adminv1.PaginatedRecords], error)
}, filters map[string]string) (*adminv1.PaginatedRecords, error) {
	resp, err := ds.ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{
		ModelName: "TestArticle", Page: 1, PageSize: 10, Filters: filters,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func createArticle(ctx context.Context, ds interface {
	CreateRecord(context.Context, *connect.Request[adminv1.CreateRecordRequest]) (*connect.Response[adminv1.Record], error)
}, title string) (string, error) {
	resp, err := ds.CreateRecord(ctx, connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"` + title + `"`, "Body": `"bench"`}},
	}))
	if err != nil {
		return "", err
	}
	return unquote(resp.Msg.GetValuesJson()["ID"]), nil
}

// ---- probes ----------------------------------------------------------------------------------

// FDS-01: the fleet lists the models an agent registered.
func probeFleetListsModels(t *testing.T, e *env) verdict {
	srv, _ := e.startPair(t)
	resp, err := e.dataStudio(srv.Server).ListModels(ctxFor(t), connect.NewRequest(&adminv1.ListModelsRequest{}))
	if err != nil {
		t.Logf("ListModels: %v", err)
		return absent
	}
	var names []string
	for _, m := range resp.Msg.GetModels() {
		names = append(names, m.GetName())
	}
	switch {
	case len(names) == 1 && names[0] == "TestArticle":
		return present
	case len(names) == 0:
		return absent
	}
	t.Logf("ListModels answered %v", names)
	return partial
}

// FDS-02: a record survives create, read, update and delete through the
// fleet, once its model is on the mutation allowlist.
func probeFleetCRUD(t *testing.T, e *env) verdict {
	srv, _ := e.startPair(t, "TestArticle")
	ds := e.dataStudio(srv.Server)
	ctx := ctxFor(t)

	id, err := createArticle(ctx, ds, "created through the fleet")
	if connect.CodeOf(err) == connect.CodePermissionDenied {
		t.Logf("create refused although the model is allowlisted: %v", err)
		return absent
	}
	if err != nil || id == "" {
		t.Logf("create: id=%q err=%v", id, err)
		return partial
	}
	if _, err := ds.UpdateRecord(ctx, connect.NewRequest(&adminv1.UpdateRecordRequest{
		ModelName: "TestArticle", Id: id, Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"updated"`}},
	})); err != nil {
		t.Logf("update: %v", err)
		return partial
	}
	got, err := ds.GetRecord(ctx, connect.NewRequest(&adminv1.GetRecordRequest{ModelName: "TestArticle", Id: id}))
	if err != nil || unquote(got.Msg.GetValuesJson()["Title"]) != "updated" {
		t.Logf("read after update: err=%v values=%v", err, got.Msg.GetValuesJson())
		return partial
	}
	del, err := ds.DeleteRecord(ctx, connect.NewRequest(&adminv1.DeleteRecordRequest{ModelName: "TestArticle", Id: id}))
	if err != nil || !del.Msg.GetDeleted() {
		t.Logf("delete: err=%v deleted=%v", err, del.Msg.GetDeleted())
		return partial
	}
	if _, err := ds.GetRecord(ctx, connect.NewRequest(&adminv1.GetRecordRequest{ModelName: "TestArticle", Id: id})); err == nil {
		t.Log("the deleted record still reads back")
		return partial
	}
	return present
}

// FDS-03: mutations are refused by default and only the allowlist opens
// them; reads are never gated.
func probeMutationsDenyByDefault(t *testing.T, e *env) verdict {
	ctx := ctxFor(t)
	closed, _ := e.startPair(t)
	ds := e.dataStudio(closed.Server)
	if _, err := listArticles(ctx, ds, nil); err != nil {
		t.Fatalf("reads must work without an allowlist: %v", err)
	}
	_, err := createArticle(ctx, ds, "gated")
	switch {
	case err == nil:
		t.Log("a mutation went through with no allowlist configured")
		return absent
	case connect.CodeOf(err) != connect.CodePermissionDenied:
		t.Logf("the refusal is not PermissionDenied: %v", err)
		return partial
	}

	open, _ := e.startPair(t, "TestArticle")
	if _, err := createArticle(ctx, e.dataStudio(open.Server), "allowed"); err != nil {
		t.Logf("the allowlist did not open the model: %v", err)
		return partial
	}
	return present
}

// FDS-04: a viewer operator (read-only role from the trusted proxy) can
// read and cannot mutate, even with every model allowlisted.
func probeViewerCannotMutate(t *testing.T, e *env) verdict {
	ctx := ctxFor(t)
	srv, _ := e.startPair(t, "*")
	viewer := e.dataStudioAs(srv.Server, viewerHeaders())
	if _, err := listArticles(ctx, viewer, nil); err != nil {
		t.Logf("the viewer cannot even read: %v", err)
		return partial
	}
	_, err := createArticle(ctx, viewer, "by a viewer")
	switch {
	case err == nil:
		t.Log("a viewer created a record")
		return absent
	case connect.CodeOf(err) != connect.CodePermissionDenied:
		t.Logf("the refusal is not PermissionDenied: %v", err)
		return partial
	}
	return present
}

// FDS-05: the operator identity crosses the stream. Measured on the
// contract: does DataStudioRequest, or any request body it wraps, declare
// a field for who is asking. A field that appears flips this probe to
// partial — red against the recorded verdict — and the probe then has to
// grow a check that the agent receives it filled.
func probeIdentityCrossesStream(t *testing.T, e *env) verdict {
	who := []string{"subject", "operator", "identity", "actor", "principal", "user", "caller"}
	req := messageNamed(t, "DataStudioRequest")
	found := fieldsContaining(req, who...)
	fs := req.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Kind() != protoreflect.MessageKind {
			continue
		}
		for _, n := range fieldsContaining(f.Message(), who...) {
			found = append(found, string(f.Message().Name())+"."+n)
		}
	}
	if len(found) > 0 {
		t.Logf("the wire declares %v: extend this probe to check the agent receives it filled", found)
		return partial
	}
	return absent
}

// denyEverything is a policy source whose only rule denies every action
// on the article model. It is what an application's RBAC would say if the
// fleet operator were not allowed near the data.
type denyEverything struct{}

func (denyEverything) GetPolicy() ([][]string, error) {
	return [][]string{{"*", "admin:test_articles", "*", "deny"}, {"*", "admin:TestArticle", "*", "deny"}}, nil
}
func (denyEverything) GetGroupingPolicy() ([][]string, error) { return nil, nil }
func (denyEverything) GetAllRoles() ([]string, error)         { return nil, nil }

// FDS-06: per-model authorization by the application's policy applies to
// the fleet operator.
func probeAppPolicyAppliesToFleet(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	d, reg := e.agentDB(t, false)
	ag := e.startAgent(t, agent.Config{
		Endpoints:  []string{"http://" + srv.AgentAddr()},
		Registry:   reg,
		Databases:  map[string]*db.DB{"default": d},
		Authorizer: denyEverything{},
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	page, err := listArticles(ctxFor(t), e.dataStudio(srv.Server), nil)
	switch {
	case connect.CodeOf(err) == connect.CodePermissionDenied:
		return present
	case err != nil:
		t.Logf("ListRecords failed for another reason: %v", err)
		return partial
	case len(page.GetItems()) > 0:
		t.Logf("%d rows came back through a policy that denies the model", len(page.GetItems()))
		return absent
	}
	t.Log("no error and no rows: the policy neither refused nor let through")
	return partial
}

// FDS-07: fleet reads are tenant-filtered when the model declares a
// tenant column and the operator is scoped to one tenant.
func probeTenantFilteredReads(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	d, reg := e.agentDB(t, true)
	ag := e.startAgent(t, agent.Config{
		Endpoints: []string{"http://" + srv.AgentAddr()},
		Registry:  reg,
		Databases: map[string]*db.DB{"default": d},
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	// The only place an operator's tenant could ride today is a
	// trusted-proxy header; there is no configured name for one, so the
	// obvious spelling is sent and the answer shows whether anything reads it.
	scoped := e.dataStudioAs(srv.Server, map[string]string{"X-Auth-Tenant": "a"})
	resp, err := scoped.ListRecords(ctxFor(t), connect.NewRequest(&adminv1.ListRecordsRequest{
		ModelName: "TestTenantArticle", Page: 1, PageSize: 10,
	}))
	if err != nil {
		t.Fatalf("ListRecords TestTenantArticle: %v", err)
	}
	items := resp.Msg.GetItems()
	tenants := map[string]int{}
	for _, it := range items {
		tenants[unquote(it.GetValuesJson()["TenantID"])]++
	}
	t.Logf("server.Config tenant knobs: %v; wire tenant fields: %v",
		fieldsNamed(server.Config{}, "", "Tenant"), anyFieldContaining("tenant"))
	switch {
	case len(items) == 3:
		t.Logf("every tenant's rows came back: %v", tenants)
		return absent
	case len(items) == 2 && tenants["a"] == 2:
		return present
	}
	t.Logf("unexpected page: %d rows, tenants %v", len(items), tenants)
	return partial
}

// FDS-08: filters with operators over the wire. Equality is what the
// map<string,string> carries; an operator spelling is what the contract's
// filter language needs.
func probeFilterOperatorsOverWire(t *testing.T, e *env) verdict {
	ctx := ctxFor(t)
	srv, _ := e.startPair(t)
	ds := e.dataStudio(srv.Server)

	eq, err := listArticles(ctx, ds, map[string]string{"Title": "seed article 1"})
	if err != nil {
		t.Logf("equality filter: %v", err)
		return absent
	}
	if n := len(eq.GetItems()); n != 1 {
		t.Logf("equality filter returned %d rows, not 1: filters are ignored", n)
		return absent
	}
	for _, key := range []string{"Title__contains", "title__contains"} {
		op, err := listArticles(ctx, ds, map[string]string{key: "article 1"})
		if err != nil {
			t.Logf("operator spelling %q refused: %v", key, err)
			continue
		}
		if len(op.GetItems()) == 1 && unquote(op.GetItems()[0].GetValuesJson()["Title"]) == "seed article 1" {
			return present
		}
		t.Logf("operator spelling %q returned %d rows: the operator was dropped", key, len(op.GetItems()))
	}
	return partial
}

// FDS-09: pagination carries an exact total, filtered or not.
func probePaginationExactTotal(t *testing.T, e *env) verdict {
	ctx := ctxFor(t)
	srv, _ := e.startPair(t)
	ds := e.dataStudio(srv.Server)

	all, err := ds.ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{ModelName: "TestArticle", Page: 1, PageSize: 2}))
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	exactAll := all.Msg.GetTotal() == 3 && !all.Msg.GetTotalEstimated()
	filtered, err := listArticles(ctx, ds, map[string]string{"Title": "seed article 1"})
	if err != nil {
		t.Fatalf("ListRecords filtered: %v", err)
	}
	exactFiltered := filtered.GetTotal() == 1 && !filtered.GetTotalEstimated()
	t.Logf("unfiltered total=%d estimated=%v; filtered total=%d estimated=%v",
		all.Msg.GetTotal(), all.Msg.GetTotalEstimated(), filtered.GetTotal(), filtered.GetTotalEstimated())
	switch {
	case exactAll && exactFiltered:
		return present
	case exactAll || exactFiltered:
		return partial
	}
	return absent
}

// FDS-10: the agent serves Data Studio through the datasource contract.
// Two facts: the agent module requires the module that owns the contract,
// and agent.Config takes a value typed from it.
func probeAgentSpeaksDatasource(t *testing.T, e *env) verdict {
	const rootModule = "github.com/jcsvwinston/orbit"
	const contractPkg = rootModule + "/datasource"

	requiresRoot := false
	for _, line := range strings.Split(e.readFile(t, "agent/go.mod"), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == rootModule {
			requiresRoot = true
		}
	}
	hasField := false
	rt := reflect.TypeOf(agent.Config{})
	for i := 0; i < rt.NumField(); i++ {
		ty := rt.Field(i).Type
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice || ty.Kind() == reflect.Map {
			ty = ty.Elem()
		}
		if ty.PkgPath() == contractPkg {
			hasField = true
		}
	}
	t.Logf("agent/go.mod requires the root module: %v; agent.Config has a datasource-typed field: %v", requiresRoot, hasField)
	switch {
	case requiresRoot && hasField:
		return present
	case requiresRoot || hasField:
		return partial
	}
	return absent
}

// FDS-11: a fleet mutation leaves an audit entry with operator, model,
// record and node, and the entry carries what changed.
func probeFleetAuditEntry(t *testing.T, e *env) verdict {
	ctx := ctxFor(t)
	srv, ag := e.startPair(t, "TestArticle")
	id, err := createArticle(ctx, e.dataStudio(srv.Server), "audited")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	resp, err := e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil {
		t.Logf("ListAudit: %v", err)
		return absent
	}
	entries := resp.Msg.GetEntries()
	if len(entries) == 0 {
		return absent
	}
	en := entries[0]
	attributed := en.GetActor() == operatorName && en.GetAction() == "datastudio.create" &&
		strings.Contains(en.GetTarget(), "TestArticle") && strings.Contains(en.GetTarget(), "#"+id) &&
		en.GetNodeId() == ag.NodeID() && en.GetTime() != nil
	if !attributed {
		t.Logf("entry is incomplete: actor=%q action=%q target=%q node=%q", en.GetActor(), en.GetAction(), en.GetTarget(), en.GetNodeId())
		return partial
	}
	diff := fieldsContaining(messageNamed(t, "AuditEntry"), "before", "after", "old", "new", "diff", "previous", "values")
	if len(diff) == 0 {
		t.Log("AuditEntry has no before/after fields")
		return partial
	}
	return present
}

// FDS-12: the direction the fleet's Data Studio is meant to take
// (docs/adrs/ADR-002) is recorded as implemented, in the ADR and in the
// index that lists it.
func probeADR002RecordedImplemented(t *testing.T, e *env) verdict {
	done := func(s string) bool {
		return containsAny(s, []string{"implemented", "implementado", "implementada", "closed", "cerrado", "cerrada", "done"})
	}
	status := ""
	for _, line := range strings.Split(e.readFile(t, "docs/adrs/ADR-002-fleet-datastudio-identidad.md"), "\n") {
		if strings.HasPrefix(line, "status:") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
			break
		}
	}
	if status == "" {
		t.Fatal("ADR-002 has no status: line in its front matter")
	}
	cell := ""
	for _, line := range strings.Split(e.readFile(t, "docs/adrs/README.md"), "\n") {
		if !strings.Contains(line, "ADR-002") || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) >= 4 {
			cell = strings.TrimSpace(cells[3])
		}
		break
	}
	if cell == "" {
		t.Fatal("docs/adrs/README.md has no table row for ADR-002")
	}
	adrDone := done(status)
	rowDone := done(cell) && !containsAny(cell, []string{"pendiente", "pending"})
	t.Logf("ADR status %q; index row status %q", status, cell)
	switch {
	case adrDone && rowDone:
		return present
	case adrDone || rowDone:
		return partial
	}
	return absent
}
