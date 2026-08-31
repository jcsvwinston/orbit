package server_test

// AO-3 (fleet-plane audit): the fleet Data Studio executes mutations on
// the agent's database WITHOUT the application's per-model RBAC or
// tenant filtering (no operator identity crosses the bidi stream). The
// server therefore gates mutations behind an explicit per-model
// allowlist (Config.DataStudioAllowedModels), deny-by-default: with no
// list configured, every Data Studio mutation is refused. Reads keep
// working — the fleet plane is an observability surface first.

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// TestDataStudio_MutationsDeniedWithoutAllowlist pins the deny-by-default
// posture: a server with no DataStudioAllowedModels configured refuses
// every mutation with PermissionDenied, while reads still work.
func TestDataStudio_MutationsDeniedWithoutAllowlist(t *testing.T) {
	srv, _, stop := startServerAndAgent(t)
	defer stop()
	client := newDataStudioClient(t, "http://"+srv.UIAddr())

	// Reads are not gated.
	if _, err := client.ListRecords(context.Background(), connect.NewRequest(&adminv1.ListRecordsRequest{
		ModelName: "TestArticle",
	})); err != nil {
		t.Fatalf("ListRecords without allowlist: %v", err)
	}

	_, err := client.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"gated"`}},
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("CreateRecord without allowlist = %v, want PermissionDenied", err)
	}

	_, err = client.UpdateRecord(context.Background(), connect.NewRequest(&adminv1.UpdateRecordRequest{
		ModelName: "TestArticle",
		Id:        "1",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"gated"`}},
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("UpdateRecord without allowlist = %v, want PermissionDenied", err)
	}

	_, err = client.DeleteRecord(context.Background(), connect.NewRequest(&adminv1.DeleteRecordRequest{
		ModelName: "TestArticle",
		Id:        "1",
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("DeleteRecord without allowlist = %v, want PermissionDenied", err)
	}

	_, err = client.BulkAction(context.Background(), connect.NewRequest(&adminv1.BulkActionRequest{
		ModelName: "TestArticle",
		Action:    "delete",
		Ids:       []string{"1"},
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("BulkAction without allowlist = %v, want PermissionDenied", err)
	}
}

// TestDataStudio_ModelOutsideAllowlistDenied: a configured allowlist only
// opens the models it names.
func TestDataStudio_ModelOutsideAllowlistDenied(t *testing.T) {
	srv, _, stop := startServerAndAgent(t, "SomeOtherModel")
	defer stop()
	client := newDataStudioClient(t, "http://"+srv.UIAddr())

	_, err := client.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"gated"`}},
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("CreateRecord outside allowlist = %v, want PermissionDenied", err)
	}
}

// TestDataStudio_WildcardAllowlistAllowsMutations: "*" restores the
// previous mutate-anything behaviour, but only as an explicit opt-in.
func TestDataStudio_WildcardAllowlistAllowsMutations(t *testing.T) {
	srv, _, stop := startServerAndAgent(t, "*")
	defer stop()
	client := newDataStudioClient(t, "http://"+srv.UIAddr())

	created, err := client.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "TestArticle",
		Record: &adminv1.Record{ValuesJson: map[string]string{
			"Title": `"wildcard"`,
			"Body":  `"allowed"`,
		}},
	}))
	if err != nil {
		t.Fatalf("CreateRecord with wildcard allowlist: %v", err)
	}
	if created.Msg.GetValuesJson()["Title"] != `"wildcard"` {
		t.Fatalf("unexpected created record: %v", created.Msg.GetValuesJson())
	}
}
