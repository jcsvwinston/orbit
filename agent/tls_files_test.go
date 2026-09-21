package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// pemFiles mints a throwaway CA and a client leaf for cn and writes the
// three PEM files a deployment would ship: ca.crt, agent.crt, agent.key.
func pemFiles(t *testing.T, cn string) (caFile, certFile, keyFile string) {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "agent-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, typ string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return write("ca.crt", "CERTIFICATE", caDER), write("agent.crt", "CERTIFICATE", leafDER), write("agent.key", "EC PRIVATE KEY", keyDER)
}

func TestExtensionConfig_TLSConfig_NoFiles_ReturnsTLSAsGiven(t *testing.T) {
	got, err := ExtensionConfig{}.TLSConfig()
	if err != nil || got != nil {
		t.Fatalf("no TLS and no files: got %v, %v; want nil, nil (system trust store)", got, err)
	}
	given := &tls.Config{ServerName: "given"}
	got, err = ExtensionConfig{TLS: given}.TLSConfig()
	if err != nil || got != given {
		t.Fatalf("TLS without files must be returned as is, got %v, %v", got, err)
	}
}

func TestExtensionConfig_TLSConfig_LoadsTheFiles(t *testing.T) {
	ca, cert, key := pemFiles(t, "node-from-files")
	cfg, err := ExtensionConfig{TLSCertFile: cert, TLSKeyFile: key, TLSCAFile: ca, TLSServerName: "admin.internal"}.TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if cfg.GetClientCertificate == nil {
		t.Fatal("the client certificate must be served through GetClientCertificate (so a rotation on disk reaches the next handshake)")
	}
	presented, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil || presented == nil || presented.Leaf == nil {
		t.Fatalf("GetClientCertificate: %v, %+v", err, presented)
	}
	if cn := presented.Leaf.Subject.CommonName; cn != "node-from-files" {
		t.Fatalf("client certificate CN = %q", cn)
	}
	if cfg.RootCAs == nil {
		t.Fatal("tls_ca_file did not become RootCAs")
	}
	if cfg.ServerName != "admin.internal" {
		t.Fatalf("ServerName = %q", cfg.ServerName)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2 floor", cfg.MinVersion)
	}
}

func TestExtensionConfig_TLSConfig_FilesLoadOnTopOfTLS_WithoutMutatingIt(t *testing.T) {
	_, cert, key := pemFiles(t, "node-x")
	given := &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: false, Certificates: []tls.Certificate{{}}}
	cfg, err := ExtensionConfig{TLS: given, TLSCertFile: cert, TLSKeyFile: key}.TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if cfg == given {
		t.Fatal("the files must be loaded onto a clone, not onto the caller's value")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatal("the clone lost a setting the caller made")
	}
	if len(cfg.Certificates) != 0 || cfg.GetClientCertificate == nil {
		t.Fatal("the file certificate must replace the one TLS carried: Certificates cleared, GetClientCertificate serving the files")
	}
	if presented, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{}); err != nil || presented.Leaf.Subject.CommonName != "node-x" {
		t.Fatalf("GetClientCertificate presents %v, %v; want node-x", presented, err)
	}
	if len(given.Certificates) != 1 || given.GetClientCertificate != nil {
		t.Fatal("the caller's tls.Config was mutated")
	}
}

func TestExtensionConfig_TLSConfig_Refusals(t *testing.T) {
	ca, cert, key := pemFiles(t, "node-x")
	cases := map[string]ExtensionConfig{
		"cert_without_key": {TLSCertFile: cert},
		"key_without_cert": {TLSKeyFile: key},
		"missing_cert":     {TLSCertFile: filepath.Join(t.TempDir(), "absent.crt"), TLSKeyFile: key},
		"ca_without_pem":   {TLSCAFile: key + ".notpem"},
	}
	if err := os.WriteFile(cases["ca_without_pem"].TLSCAFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.TLSConfig(); err == nil {
				t.Fatalf("%s: TLSConfig accepted %+v", name, c)
			}
		})
	}
	// The pair together with the CA is what the refusals above are not.
	if _, err := (ExtensionConfig{TLSCertFile: cert, TLSKeyFile: key, TLSCAFile: ca}).TLSConfig(); err != nil {
		t.Fatalf("the complete configuration was refused: %v", err)
	}
}

func TestExtensionConfig_CertificateNodeID(t *testing.T) {
	_, cert, key := pemFiles(t, "node-from-cn")
	got, err := ExtensionConfig{TLSCertFile: cert, TLSKeyFile: key}.certificateNodeID()
	if err != nil || got != "node-from-cn" {
		t.Fatalf("certificateNodeID = %q, %v", got, err)
	}
	got, err = ExtensionConfig{}.certificateNodeID()
	if err != nil || got != "" {
		t.Fatalf("without a certificate file the node keeps its resolved id; got %q, %v", got, err)
	}
}

// TestExtension_Attach_NamesTheNodeAfterTheCertificate is the contract the
// bench measures end to end (IDENT-05): certificate files in the
// configuration, no node_id, and the agent registers as the certificate's
// Common Name. The endpoint here is unreachable on purpose — Attach is
// fail-open — so the assertion is on the NodeID the agent resolved.
func TestExtension_Attach_NamesTheNodeAfterTheCertificate(t *testing.T) {
	ca, cert, key := pemFiles(t, "node-named-by-cert")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	newApp := func() *app.App {
		return &app.App{Logger: logger, Observability: observability.NewBus(logger)}
	}

	t.Run("no_node_id_takes_the_cn", func(t *testing.T) {
		ext := NewExtension(ExtensionConfig{
			Endpoints:   []string{"https://127.0.0.1:1"},
			TLSCertFile: cert, TLSKeyFile: key, TLSCAFile: ca,
		}, t.TempDir(), "v0.0.0-test").(*extension)
		if err := ext.Attach(newApp()); err != nil {
			t.Fatalf("Attach: %v", err)
		}
		defer func() { _ = ext.Shutdown(context.Background()) }()
		if got := ext.agent.NodeID(); got != "node-named-by-cert" {
			t.Fatalf("NodeID = %q, want the certificate's Common Name", got)
		}
	})

	t.Run("node_id_set_wins", func(t *testing.T) {
		ext := NewExtension(ExtensionConfig{
			Endpoints:      []string{"https://127.0.0.1:1"},
			TLSCertFile:    cert,
			TLSKeyFile:     key,
			NodeIDOverride: "operator-chose-this",
		}, t.TempDir(), "v0.0.0-test").(*extension)
		if err := ext.Attach(newApp()); err != nil {
			t.Fatalf("Attach: %v", err)
		}
		defer func() { _ = ext.Shutdown(context.Background()) }()
		if got := ext.agent.NodeID(); got != "operator-chose-this" {
			t.Fatalf("NodeID = %q, want the explicit node_id", got)
		}
	})

	t.Run("broken_files_fail_the_boot", func(t *testing.T) {
		ext := NewExtension(ExtensionConfig{
			Endpoints:   []string{"https://127.0.0.1:1"},
			TLSCertFile: cert, // no key
		}, t.TempDir(), "v0.0.0-test")
		err := ext.Attach(newApp())
		if err == nil || !strings.Contains(err.Error(), "tls_key_file") {
			t.Fatalf("a half-configured certificate must fail Attach naming the missing key, got %v", err)
		}
	})
}
