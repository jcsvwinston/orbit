package fleetbench

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jcsvwinston/quark"
	// The module, not the bare driver: it registers the driver and Quark's
	// error classification predicates (quark ADR-0023).
	_ "github.com/jcsvwinston/quark/drivers/sqlite"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	"github.com/jcsvwinston/orbit/quarkdatasource"
	server "github.com/jcsvwinston/orbit/server"
)

// QuarkNote is a Quark model with a tenant column, the way an application on
// the Quark ORM declares one.
type QuarkNote struct {
	ID    int64  `db:"id" pk:"true"`
	Org   string `db:"org" quark:"not_null"`
	Title string `db:"title" quark:"not_null"`
}

// The last step of ADR-002's plan: quarkdatasource in the fleet. An agent
// started with the Quark adapter as its DataSource serves the Quark models
// through the admin server, as the operator the server sends — the tenant
// stamped on a create, another tenant's rows invisible — and the mutation
// leaves an audit entry that says what was written. No Nucleus registry, no
// Nucleus database handle: the fleet's Data Studio is not Nucleus-shaped.
func TestQuarkDataSourceInTheFleet(t *testing.T) {
	e := newEnv(t)
	ctx := ctxFor(t)

	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterModel(&QuarkNote{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	if err := client.MigrateRegistered(ctx); err != nil {
		t.Fatalf("MigrateRegistered: %v", err)
	}
	ds := quarkdatasource.New(client, quarkdatasource.WithTenantColumn("org"))
	if err := quarkdatasource.Register[QuarkNote](ds); err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv := e.startServer(t, server.Config{DataStudioAllowedModels: []string{"QuarkNote"}})
	ag := e.startAgent(t, agent.Config{
		Endpoints:  []string{"http://" + srv.AgentAddr()},
		DataSource: ds, // and nothing of Nucleus
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("the agent did not register in 4s")
	}

	models, err := e.dataStudio(srv.Server).ListModels(ctx, connect.NewRequest(&adminv1.ListModelsRequest{}))
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if n := len(models.Msg.GetModels()); n != 1 || models.Msg.GetModels()[0].GetName() != "QuarkNote" {
		t.Fatalf("the fleet lists the Quark model, got %v", models.Msg.GetModels())
	}

	acme := e.dataStudioAs(srv.Server, map[string]string{"X-Auth-Tenant": "acme"})
	created, err := acme.CreateRecord(ctx, connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "QuarkNote",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"from the fleet"`}},
	}))
	if err != nil {
		t.Fatalf("CreateRecord as acme: %v", err)
	}
	if got := created.Msg.GetValuesJson()["Org"]; got != `"acme"` {
		t.Fatalf("the operator's tenant is stamped on the record, got Org=%s in %v", got, created.Msg.GetValuesJson())
	}

	globex := e.dataStudioAs(srv.Server, map[string]string{"X-Auth-Tenant": "globex"})
	page, err := globex.ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{ModelName: "QuarkNote"}))
	if err != nil {
		t.Fatalf("ListRecords as globex: %v", err)
	}
	if len(page.Msg.GetItems()) != 0 || page.Msg.GetTotal() != 0 {
		t.Fatalf("another tenant sees nothing, got %d items, total %d", len(page.Msg.GetItems()), page.Msg.GetTotal())
	}
	page, err = acme.ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{ModelName: "QuarkNote"}))
	if err != nil {
		t.Fatalf("ListRecords as acme: %v", err)
	}
	if len(page.Msg.GetItems()) != 1 || page.Msg.GetTotal() != 1 {
		t.Fatalf("the owning tenant sees its row, got %d items, total %d", len(page.Msg.GetItems()), page.Msg.GetTotal())
	}

	audit, err := e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(audit.Msg.GetEntries()) == 0 {
		t.Fatal("the create left no audit entry")
	}
	en := audit.Msg.GetEntries()[0]
	if en.GetActor() != operatorName || en.GetAction() != "datastudio.create" || !strings.Contains(en.GetTarget(), "QuarkNote") {
		t.Fatalf("the entry is attributed to the operator, the action and the model: %+v", en)
	}
	if !strings.Contains(en.GetAfterJson(), `"acme"`) || !strings.Contains(en.GetAfterJson(), `"from the fleet"`) {
		t.Fatalf("the entry says what was written, got after_json=%s", en.GetAfterJson())
	}
}
