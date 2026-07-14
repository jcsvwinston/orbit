package server_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// TestServer_GetSelf verifies the identity echo: the UI client injects
// X-Auth-User=test-operator (uiH2CClient), so GetSelf must reflect it and
// carry a server version string.
func TestServer_GetSelf(t *testing.T) {
	srv, stop := startServer(t)
	defer stop()

	uiClient := adminv1connect.NewControlServiceClient(uiH2CClient(), "http://"+srv.UIAddr())
	resp, err := uiClient.GetSelf(context.Background(), connect.NewRequest(&adminv1.GetSelfRequest{}))
	if err != nil {
		t.Fatalf("GetSelf: %v", err)
	}
	if resp.Msg.Subject != "test-operator" {
		t.Errorf("Subject = %q, want test-operator", resp.Msg.Subject)
	}
	if resp.Msg.Role != "ui-operator" {
		t.Errorf("Role = %q, want ui-operator", resp.Msg.Role)
	}
	if resp.Msg.ReadOnly {
		t.Error("ReadOnly = true, want false (default operator)")
	}
	if resp.Msg.ServerVersion == "" {
		t.Error("ServerVersion is empty; want a version string (e.g. devel)")
	}
}
