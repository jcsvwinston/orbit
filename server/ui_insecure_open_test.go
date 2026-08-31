package server_test

// OH-7 (fleet-plane audit): without a header-setting reverse proxy the
// embedded SPA could never load in a browser — every credential-less
// request got 401, including index.html. --ui-insecure-open is the
// explicit, loopback-only dev escape hatch; these tests pin its guard
// and its behaviour.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	server "github.com/jcsvwinston/orbit/server"
)

// TestServer_UIInsecureOpen_RefusesNonLoopback: fail-closed — the flag
// must never combine with a UI listener reachable from off-host.
func TestServer_UIInsecureOpen_RefusesNonLoopback(t *testing.T) {
	srv := server.New(server.Config{
		AgentAddr:      "127.0.0.1:0",
		UIAddr:         ":0", // binds every interface
		UIInsecureOpen: true,
		Logger:         discardLogger(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := srv.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "ui-insecure-open") {
		t.Fatalf("Run with UIInsecureOpen on non-loopback UI addr = %v, want refusal", err)
	}
}

// TestServer_UIInsecureOpen_ServesCredentiallessLoopback: with the flag
// set and a loopback listener, a plain browser-style GET (no headers)
// reaches the UI instead of 401.
func TestServer_UIInsecureOpen_ServesCredentiallessLoopback(t *testing.T) {
	srv, stop := startServerCfg(t, server.Config{UIInsecureOpen: true})
	defer stop()

	resp, err := http.Get("http://" + srv.UIAddr() + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET / with UIInsecureOpen = %d (%s), want 200", resp.StatusCode, string(body))
	}
}

// TestServer_UIInsecureOpen_WrongBearerStillRejected: a request that
// PRESENTS a credential and fails must not fall through to the open
// path.
func TestServer_UIInsecureOpen_WrongBearerStillRejected(t *testing.T) {
	srv, stop := startServerCfg(t, server.Config{UIInsecureOpen: true, UIBearerToken: "right-token"})
	defer stop()

	req, err := http.NewRequest(http.MethodGet, "http://"+srv.UIAddr()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET / with wrong bearer under UIInsecureOpen = %d, want 401", resp.StatusCode)
	}
}
