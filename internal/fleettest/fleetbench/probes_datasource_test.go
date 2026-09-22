// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"

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

// identityFieldName reports whether a protocol field is named for who is
// asking: an exact name (user, user_id, subject, operator, identity, actor,
// principal, caller) or a compound built on one of them. A bare substring
// would take user_agent for an identity.
func identityFieldName(name string) bool {
	exact := map[string]bool{"user": true, "user_id": true, "subject": true, "operator": true, "identity": true,
		"actor": true, "principal": true, "caller": true}
	if exact[name] {
		return true
	}
	for _, w := range []string{"subject", "operator", "identity", "actor", "principal"} {
		if strings.HasPrefix(name, w+"_") || strings.HasSuffix(name, "_"+w) {
			return true
		}
	}
	return false
}

// FDS-05: the operator identity crosses the stream and reaches the code
// the application runs on the agent. Two facts: the contract declares a
// field for who is asking (DataStudioRequest or a request body it wraps),
// and a lifecycle hook the application attached to its own model sees the
// framework's identity in its context when the fleet operator writes
// through it. The hook is on Create because the model layer has no read
// hook; a mutation is also where the identity matters most.
func probeIdentityCrossesStream(t *testing.T, e *env) verdict {
	var declared []string
	req := messageNamed(t, "DataStudioRequest")
	collect := func(md protoreflect.MessageDescriptor, prefix string) {
		fs := md.Fields()
		for i := 0; i < fs.Len(); i++ {
			if name := string(fs.Get(i).Name()); identityFieldName(name) {
				declared = append(declared, prefix+name)
			}
		}
	}
	collect(req, "")
	fs := req.Fields()
	for i := 0; i < fs.Len(); i++ {
		if f := fs.Get(i); f.Kind() == protoreflect.MessageKind {
			collect(f.Message(), string(f.Message().Name())+".")
		}
	}

	var seen atomic.Pointer[string]
	d, reg := e.agentDBWith(t, false, model.ModelConfig{
		BeforeCreate: func(hc model.HookContext, _ interface{}) error {
			subject := ""
			if claims, ok := auth.ClaimsFromContext(hc.Context); ok && claims != nil {
				subject = claims.UserID
				if subject == "" {
					subject = claims.Username
				}
			}
			seen.Store(&subject)
			return nil
		},
	})
	srv := e.startServer(t, server.Config{DataStudioAllowedModels: []string{"TestArticle"}})
	ag := e.startAgent(t, agent.Config{
		Endpoints: []string{"http://" + srv.AgentAddr()},
		Registry:  reg,
		Databases: map[string]*db.DB{"default": d},
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	if _, err := createArticle(ctxFor(t), e.dataStudio(srv.Server), "who wrote this"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got := seen.Load()
	if got == nil {
		t.Fatal("the BeforeCreate hook never ran: the write did not go through the model layer")
	}
	t.Logf("wire identity fields: %v; identity seen by the agent-side hook: %q", declared, *got)
	switch {
	case len(declared) > 0 && *got == operatorName:
		return present
	case len(declared) > 0 || *got != "":
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
	knobs := fieldsNamed(server.Config{}, "", "Tenant")
	wire := anyFieldContaining("tenant")
	t.Logf("server.Config tenant knobs: %v; wire tenant fields: %v", knobs, wire)
	switch {
	case len(items) == 2 && tenants["a"] == 2:
		return present
	case len(items) == 3 && len(knobs) == 0 && len(wire) == 0:
		t.Logf("every tenant's rows came back: %v", tenants)
		return absent
	case len(items) == 3:
		t.Logf("every tenant's rows came back (%v) although a tenant surface exists: extend this probe to use it", tenants)
		return partial
	}
	t.Logf("unexpected page: %d rows, tenants %v", len(items), tenants)
	return partial
}

// FDS-08: filters with operators over the wire. Equality is what the
// map<string,string> carries; an operator is what `where` carries since
// A9 S3, and the agent applies it through the model layer's Where — or
// refuses it, never drops it.
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

	// The typed field, when the descriptor declares it.
	if len(fieldsContaining(messageNamed(t, "ListRecordsRequest"), "where")) == 0 {
		// The map is the only wire form: an operator spelling in it is not
		// a column and is dropped, which is the partial the note records.
		for _, key := range []string{"Title__contains", "title__contains"} {
			op, err := listArticles(ctx, ds, map[string]string{key: "article 1"})
			if err != nil {
				t.Logf("operator spelling %q refused: %v", key, err)
				continue
			}
			t.Logf("operator spelling %q returned %d rows: the operator was dropped", key, len(op.GetItems()))
		}
		return partial
	}
	listWhere := func(where ...*adminv1.RecordFilter) (*adminv1.PaginatedRecords, error) {
		resp, err := ds.ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{
			ModelName: "TestArticle", Page: 1, PageSize: 25, Where: where,
		}))
		if err != nil {
			return nil, err
		}
		return resp.Msg, nil
	}
	// contains narrows to the one row; a set keeps two; a null check on a
	// column that is never null keeps none.
	contains, err := listWhere(&adminv1.RecordFilter{Column: "Title", Op: "contains", Value: "article 1"})
	if err != nil {
		t.Logf("where contains refused: %v", err)
		return partial
	}
	if n := len(contains.GetItems()); n != 1 || unquote(contains.GetItems()[0].GetValuesJson()["Title"]) != "seed article 1" {
		t.Logf("where contains returned %d rows, want the one article", n)
		return partial
	}
	set, err := listWhere(&adminv1.RecordFilter{Column: "Title", Op: "in", Values: []string{"seed article 1", "seed article 2"}})
	if err != nil || len(set.GetItems()) != 2 {
		t.Logf("where in returned %d rows (%v), want 2", len(set.GetItems()), err)
		return partial
	}
	null, err := listWhere(&adminv1.RecordFilter{Column: "Title", Op: "isnull", Value: "true"})
	if err != nil || len(null.GetItems()) != 0 {
		t.Logf("where isnull=true returned %d rows (%v), want 0", len(null.GetItems()), err)
		return partial
	}
	// And the rule that makes a filter trustworthy: an operator the agent
	// does not know is refused, not treated as "no filter".
	if unknown, err := listWhere(&adminv1.RecordFilter{Column: "Title", Op: "like", Value: "%"}); err == nil {
		t.Logf("an unknown operator was accepted and returned %d rows: a dropped filter looks like a result", len(unknown.GetItems()))
		return partial
	}
	return present
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
// Two facts: the agent module requires the module that owns the contract
// (its own module since ADR-012: github.com/jcsvwinston/orbit/datasource —
// requiring the root would drag the panel in and is what ADR-006 forbids),
// and agent.Config takes a value typed from it.
func probeAgentSpeaksDatasource(t *testing.T, e *env) verdict {
	const contractModule = "github.com/jcsvwinston/orbit/datasource"

	requiresContract := false
	for _, line := range strings.Split(e.readFile(t, "agent/go.mod"), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == contractModule {
			requiresContract = true
		}
	}
	hasField := false
	rt := reflect.TypeOf(agent.Config{})
	for i := 0; i < rt.NumField(); i++ {
		if typeMentions(rt.Field(i).Type, contractModule, 0) {
			hasField = true
		}
	}
	t.Logf("agent/go.mod requires the contract module: %v; agent.Config has a datasource-typed field: %v", requiresContract, hasField)
	switch {
	case requiresContract && hasField:
		return present
	case requiresContract || hasField:
		return partial
	}
	return absent
}

// typeMentions reports whether ty is, or is built from, a type of pkg —
// through pointers, slices, maps, channels and function signatures.
func typeMentions(ty reflect.Type, pkg string, depth int) bool {
	if ty == nil || depth > 4 {
		return false
	}
	if ty.PkgPath() == pkg {
		return true
	}
	switch ty.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return typeMentions(ty.Elem(), pkg, depth+1)
	case reflect.Map:
		return typeMentions(ty.Key(), pkg, depth+1) || typeMentions(ty.Elem(), pkg, depth+1)
	case reflect.Func:
		for i := 0; i < ty.NumIn(); i++ {
			if typeMentions(ty.In(i), pkg, depth+1) {
				return true
			}
		}
		for i := 0; i < ty.NumOut(); i++ {
			if typeMentions(ty.Out(i), pkg, depth+1) {
				return true
			}
		}
	}
	return false
}

// FDS-11: a fleet mutation leaves an audit entry attributed to operator,
// model, record and node, and the entry carries what changed. An entry
// that is not attributed is no audit; one that is attributed but says
// nothing about the values is half of one.
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
		t.Log("the mutation left no audit entry")
		return absent
	}
	en := entries[0]
	var missing []string
	if en.GetActor() != operatorName {
		missing = append(missing, "actor="+en.GetActor())
	}
	if en.GetAction() != "datastudio.create" {
		missing = append(missing, "action="+en.GetAction())
	}
	if !strings.Contains(en.GetTarget(), "TestArticle") {
		missing = append(missing, "model (target="+en.GetTarget()+")")
	}
	if !strings.Contains(en.GetTarget(), "#"+id) {
		missing = append(missing, "record id (target="+en.GetTarget()+")")
	}
	if en.GetNodeId() != ag.NodeID() {
		missing = append(missing, "node="+en.GetNodeId())
	}
	if en.GetTime() == nil {
		missing = append(missing, "time")
	}
	if len(missing) > 0 {
		t.Logf("the entry is not attributed: %v", missing)
		return absent
	}
	// What changed. A create can only say what was written; an update says
	// both sides. Fields that exist but carry nothing are a declaration, not
	// an audit — that is what kept this control partial for one release.
	if en.GetAfterJson() == "" || !strings.Contains(en.GetAfterJson(), "audited") {
		t.Logf("the create's entry does not say what was written: after_json=%q", en.GetAfterJson())
		return partial
	}
	if _, err := e.dataStudio(srv.Server).UpdateRecord(ctx, connect.NewRequest(&adminv1.UpdateRecordRequest{
		ModelName: "TestArticle", Id: id,
		Record: &adminv1.Record{ValuesJson: map[string]string{"Title": `"audited, renamed"`}},
	})); err != nil {
		t.Fatalf("update: %v", err)
	}
	resp, err = e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil {
		t.Fatalf("ListAudit after update: %v", err)
	}
	up := resp.Msg.GetEntries()[0]
	if up.GetAction() != "datastudio.update" {
		t.Fatalf("newest entry is %q, want the update", up.GetAction())
	}
	before, after := up.GetBeforeJson(), up.GetAfterJson()
	switch {
	case strings.Contains(before, `"audited"`) && strings.Contains(after, `"audited, renamed"`):
		return present
	case before == "" && after == "":
		t.Log("AuditEntry declares before_json and after_json but the update's entry carries nothing in them")
		return partial
	}
	t.Logf("the update's entry does not say what changed: before_json=%q after_json=%q", before, after)
	return partial
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
		// The row whose FIRST cell links the ADR — another row may name
		// ADR-002 in its "related" column.
		if !strings.Contains(line, "[ADR-002](") || !strings.HasPrefix(strings.TrimSpace(line), "|") {
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
