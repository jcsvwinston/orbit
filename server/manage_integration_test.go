package server_test

// Integration tests for the Manage surface (W1 of the v1.2 arc): the
// RBAC snapshot routed server -> agent -> server, and the server-side
// fleet-plane audit ring fed by Data Studio mutations. Same harness as
// the Data Studio tests: real server, real agent, Connect UI client.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent"
	server "github.com/jcsvwinston/orbit/server"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// fakePolicySource implements agent/rbac.PolicySource with canned data.
type fakePolicySource struct{}

func (fakePolicySource) GetPolicy() ([][]string, error) {
	return [][]string{
		{"role:admin", "/admin/*", "*"},
		{"role:reader", "/api/articles", "GET", "deny"},
	}, nil
}

func (fakePolicySource) GetGroupingPolicy() ([][]string, error) {
	return [][]string{
		{"alice", "role:admin"},
		{"bob", "role:admin"},
		{"carol", "role:reader"},
	}, nil
}

func (fakePolicySource) GetAllRoles() ([]string, error) {
	return []string{"role:admin", "role:reader"}, nil
}

// startServerAndRbacAgent mirrors startServerAndAgent but wires an
// Authorizer instead of a model registry (RBAC-only agent).
func startServerAndRbacAgent(t *testing.T) (srv *server.Server, ag *agent.Agent, stop func()) {
	t.Helper()

	srv = server.New(server.Config{
		AgentAddr: "127.0.0.1:0",
		UIAddr:    "127.0.0.1:0",
		Logger:    discardLogger(),
	})
	srvCtx, srvCancel := context.WithCancel(context.Background())
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Run(srvCtx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.AgentAddr() == "" {
		srvCancel()
		<-srvDone
		t.Fatal("server did not bind")
	}

	bus := observability.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ag, err := agent.New(agent.Config{
		Endpoints:         []string{"http://" + srv.AgentAddr()},
		Bus:               bus,
		Authorizer:        fakePolicySource{},
		StateDir:          t.TempDir(),
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		srvCancel()
		<-srvDone
		t.Fatal(err)
	}
	agCtx, agCancel := context.WithCancel(context.Background())
	agDone := make(chan error, 1)
	go func() { agDone <- ag.Run(agCtx) }()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.State().Nodes.Lookup(ag.NodeID()); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := srv.State().Nodes.Lookup(ag.NodeID()); !ok {
		agCancel()
		srvCancel()
		t.Fatal("agent did not register with server in 3s")
	}

	stop = func() {
		agCancel()
		<-agDone
		srvCancel()
		<-srvDone
	}
	return srv, ag, stop
}

func newManageClient(uiURL string) adminv1connect.ManageServiceClient {
	return adminv1connect.NewManageServiceClient(uiH2CClient(), uiURL)
}

// TestManage_GetRbac drives the full loop: UI client -> ManageService ->
// RbacRequest over the bidi stream -> agent snapshot of the (fake)
// authorizer -> RbacResponse -> UI.
func TestManage_GetRbac(t *testing.T) {
	srv, _, stop := startServerAndRbacAgent(t)
	defer stop()

	client := newManageClient("http://" + srv.UIAddr())
	resp, err := client.GetRbac(context.Background(), connect.NewRequest(&adminv1.GetRbacRequest{}))
	if err != nil {
		t.Fatalf("GetRbac: %v", err)
	}

	roles := resp.Msg.GetRoles()
	if len(roles) != 2 {
		t.Fatalf("roles = %d, want 2 (%v)", len(roles), roles)
	}
	if roles[0].GetName() != "role:admin" || roles[0].GetMembers() != 2 {
		t.Fatalf("first role = %v, want role:admin with 2 members", roles[0])
	}
	if roles[1].GetName() != "role:reader" || roles[1].GetMembers() != 1 {
		t.Fatalf("second role = %v, want role:reader with 1 member", roles[1])
	}

	policies := resp.Msg.GetPolicies()
	if len(policies) != 2 {
		t.Fatalf("policies = %d, want 2", len(policies))
	}
	if policies[0].GetEffect() != "allow" {
		t.Fatalf("first policy effect = %q, want allow (implied)", policies[0].GetEffect())
	}
	if policies[1].GetEffect() != "deny" {
		t.Fatalf("second policy effect = %q, want deny (explicit fourth column)", policies[1].GetEffect())
	}
}

// TestManage_GetRbac_NotEnabled pins the honest error when the agent has
// no authorizer wired.
func TestManage_GetRbac_NotEnabled(t *testing.T) {
	srv, _, stop := startServerAndAgent(t) // datastudio harness: no Authorizer
	defer stop()

	client := newManageClient("http://" + srv.UIAddr())
	_, err := client.GetRbac(context.Background(), connect.NewRequest(&adminv1.GetRbacRequest{}))
	if err == nil {
		t.Fatal("GetRbac succeeded on an agent without an authorizer")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, want failed_precondition (%v)", connect.CodeOf(err), err)
	}
}

// TestManage_AuditFromDataStudioMutation verifies the fleet-plane audit:
// a Data Studio create routed through the server lands in the audit ring
// attributed to the UI operator, and ListAudit returns it newest-first.
func TestManage_AuditFromDataStudioMutation(t *testing.T) {
	srv, ag, stop := startServerAndAgent(t)
	defer stop()

	ds := newDataStudioClient(t, "http://"+srv.UIAddr())
	_, err := ds.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record: &adminv1.Record{ValuesJson: map[string]string{
			"title": `"audited"`,
		}},
	}))
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	client := newManageClient("http://" + srv.UIAddr())
	resp, err := client.ListAudit(context.Background(), connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	entries := resp.Msg.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.GetActor() != "test-operator" {
		t.Fatalf("actor = %q, want test-operator (from the trusted-proxy header)", e.GetActor())
	}
	if e.GetAction() != "datastudio.create" {
		t.Fatalf("action = %q, want datastudio.create", e.GetAction())
	}
	if e.GetNodeId() != ag.NodeID() {
		t.Fatalf("node = %q, want the routed agent %q", e.GetNodeId(), ag.NodeID())
	}
	if e.GetTime() == nil || e.GetTime().AsTime().IsZero() {
		t.Fatal("audit entry carries no timestamp")
	}
}

// Silence the unused-import guard if db types stop being needed here.
var _ = (*db.DB)(nil)
