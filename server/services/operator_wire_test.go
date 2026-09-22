package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/server/auth"
)

func TestOperatorOnTheWire(t *testing.T) {
	if got := operatorOnTheWire(context.Background()); got != nil {
		t.Fatalf("no identity on the context must put no operator on the wire, got %+v", got)
	}
	ctx := auth.WithIdentity(context.Background(), auth.Identity{
		Subject: "alice", Email: "a@example.com", Role: "ui-operator", ReadOnly: true, Tenant: "acme",
	})
	got := operatorOnTheWire(ctx)
	if got == nil {
		t.Fatal("an authenticated caller must reach the agent as an operator")
	}
	if got.GetSubject() != "alice" || got.GetEmail() != "a@example.com" || got.GetRole() != "ui-operator" || !got.GetReadOnly() || got.GetTenant() != "acme" {
		t.Fatalf("operator = %+v: every field of the identity travels", got)
	}
}

func TestAgentErrorCode(t *testing.T) {
	cases := map[string]connect.Code{
		"permission denied: \"alice\" may not list Article": connect.CodePermissionDenied,
		"Permission Denied: read-only":                      connect.CodePermissionDenied,
		"not found: record \"9\"":                           connect.CodeNotFound,
		"admin agent: model \"X\" is not registered":        connect.CodeUnknown,
		"": connect.CodeUnknown,
	}
	for msg, want := range cases {
		if got := agentErrorCode(msg); got != want {
			t.Errorf("agentErrorCode(%q) = %v, want %v", msg, got, want)
		}
	}
}
