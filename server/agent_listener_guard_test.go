package server

import (
	"crypto/tls"
	"strings"
	"testing"
)

func TestAgentListenerExposed(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{":9090", true},            // empty host → all interfaces
		{"0.0.0.0:9090", true},     // IPv4 unspecified
		{"[::]:9090", true},        // IPv6 unspecified
		{"10.0.0.5:9090", true},    // specific non-loopback IP
		{"example.com:9090", true}, // hostname we can't prove loopback
		{"127.0.0.1:9090", false},
		{"[::1]:9090", false},
		{"localhost:9090", false},
		{"127.0.0.1:0", false},
	}
	for _, tc := range cases {
		if got := agentListenerExposed(tc.addr); got != tc.want {
			t.Errorf("agentListenerExposed(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestAgentListenerGuard(t *testing.T) {
	// A certificate alone encrypts; it does not authenticate. Only a config
	// that requires and verifies client certificates counts.
	serverOnlyTLS := &tls.Config{}
	mutualTLS := &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert}

	t.Run("exposed_and_unauthenticated_is_refused", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090"}
		warn, err := cfg.agentListenerGuard()
		if err == nil {
			t.Fatal("expected refusal error, got nil")
		}
		if warn {
			t.Error("warn should be false when the listener is refused")
		}
		if !strings.Contains(err.Error(), "refusing to start") {
			t.Errorf("error should explain the refusal, got %q", err.Error())
		}
	})

	t.Run("token_allows_exposed", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", AgentToken: "s3cret"}
		if warn, err := cfg.agentListenerGuard(); err != nil || warn {
			t.Fatalf("token should permit exposed listener quietly: warn=%v err=%v", warn, err)
		}
	})

	t.Run("server_only_tls_without_token_is_refused", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", AgentTLS: serverOnlyTLS}
		warn, err := cfg.agentListenerGuard()
		if err == nil {
			t.Fatal("a server certificate without ClientAuth authenticates nobody; the guard must refuse")
		}
		if warn {
			t.Error("warn should be false when the listener is refused")
		}
		if !strings.Contains(err.Error(), "--agent-client-ca") {
			t.Errorf("error should point at the client-CA flag, got %q", err.Error())
		}
	})

	t.Run("server_only_tls_with_token_allows_exposed", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", AgentTLS: serverOnlyTLS, AgentToken: "s3cret"}
		if warn, err := cfg.agentListenerGuard(); err != nil || warn {
			t.Fatalf("TLS + token should permit exposed listener quietly: warn=%v err=%v", warn, err)
		}
	})

	t.Run("mutual_tls_allows_exposed", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", AgentTLS: mutualTLS}
		if warn, err := cfg.agentListenerGuard(); err != nil || warn {
			t.Fatalf("mutual TLS should permit exposed listener quietly: warn=%v err=%v", warn, err)
		}
	})

	t.Run("require_any_client_cert_is_not_authentication", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", AgentTLS: &tls.Config{ClientAuth: tls.RequireAnyClientCert}}
		if _, err := cfg.agentListenerGuard(); err == nil {
			t.Fatal("RequireAnyClientCert accepts unverified certificates; the guard must refuse")
		}
	})

	t.Run("loopback_allows_unauthenticated", func(t *testing.T) {
		cfg := Config{AgentAddr: "127.0.0.1:9090"}
		if warn, err := cfg.agentListenerGuard(); err != nil || warn {
			t.Fatalf("loopback should permit unauthenticated listener quietly: warn=%v err=%v", warn, err)
		}
	})

	t.Run("insecure_override_allows_with_warning", func(t *testing.T) {
		cfg := Config{AgentAddr: "0.0.0.0:9090", InsecureAgentListener: true}
		warn, err := cfg.agentListenerGuard()
		if err != nil {
			t.Fatalf("override should suppress refusal, got %v", err)
		}
		if !warn {
			t.Error("override should still request a warning")
		}
	})
}
