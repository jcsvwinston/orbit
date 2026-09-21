package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// selfSignedLeaf mints a self-signed client certificate for cn; the tests
// here read Common Names, they verify nothing.
func selfSignedLeaf(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// writePair writes leaf into certFile/keyFile with the given modification
// time, so two writes in one clock tick still differ to the stamp.
func writePair(t *testing.T, certFile, keyFile string, leaf tls.Certificate, at time.Time) {
	t.Helper()
	keyDER, err := x509.MarshalECPrivateKey(leaf.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{certFile, keyFile} {
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExtensionConfig_TLSConfig_ClientCertificateRotates is the agent half
// of "rotation without a restart": the configuration resolved once at boot
// hands the NEXT handshake whatever the files hold then.
func TestExtensionConfig_TLSConfig_ClientCertificateRotates(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "agent.crt"), filepath.Join(dir, "agent.key")
	t0 := time.Now().Add(-10 * time.Second)
	writePair(t, certFile, keyFile, selfSignedLeaf(t, "agent-v1"), t0)

	cfg, err := ExtensionConfig{TLSCertFile: certFile, TLSKeyFile: keyFile}.TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if cfg.GetClientCertificate == nil || len(cfg.Certificates) != 0 {
		t.Fatal("the client certificate must be served through GetClientCertificate, with Certificates empty so nothing else is presented")
	}
	presented := func() string {
		c, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
		if err != nil {
			t.Fatalf("GetClientCertificate: %v", err)
		}
		return c.Leaf.Subject.CommonName
	}
	if got := presented(); got != "agent-v1" {
		t.Fatalf("first handshake would present %q, want agent-v1", got)
	}

	writePair(t, certFile, keyFile, selfSignedLeaf(t, "agent-v2"), t0.Add(2*time.Second))
	if got := presented(); got != "agent-v2" {
		t.Fatalf("after rewriting the files the handshake would present %q, want agent-v2", got)
	}

	// Half a rotation keeps the previous certificate.
	v3 := selfSignedLeaf(t, "agent-v3")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: v3.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(certFile, t0.Add(4*time.Second), t0.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := presented(); got != "agent-v2" {
		t.Fatalf("with the key not yet rotated the handshake would present %q, want the previous agent-v2", got)
	}
	writePair(t, certFile, keyFile, v3, t0.Add(6*time.Second))
	if got := presented(); got != "agent-v3" {
		t.Fatalf("after the key caught up the handshake would present %q, want agent-v3", got)
	}
}
