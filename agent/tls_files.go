package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
)

// TLSConfig resolves the TLS configuration the agent dials https://
// endpoints with. It is TLS as given, with the PEM files the configuration
// names loaded on top: TLSCertFile/TLSKeyFile become the client certificate
// the agent presents, TLSCAFile the roots the server certificate is verified
// against, TLSServerName the name it is verified as. With none of the four
// set it returns TLS unchanged (nil stays nil: the system trust store).
//
// The files are read here, once. A certificate that changes on disk is
// picked up by the next connection only after the rotation the arc adds
// next; today it needs a restart.
func (c ExtensionConfig) TLSConfig() (*tls.Config, error) {
	if !c.hasTLSFiles() {
		return c.TLS, nil
	}
	var cfg *tls.Config
	if c.TLS != nil {
		cfg = c.TLS.Clone()
	} else {
		cfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return nil, errors.New("admin agent: tls_cert_file and tls_key_file must be set together")
	}
	if c.TLSCertFile != "" {
		cert, err := loadClientCertificate(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, err
		}
		// The file replaces whatever TLS carried: a deployment that names
		// its certificate in the configuration means that one.
		cfg.Certificates = []tls.Certificate{cert}
	}
	if c.TLSCAFile != "" {
		pemBytes, err := os.ReadFile(c.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("admin agent: read tls_ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("admin agent: tls_ca_file %q: no PEM certificates found", c.TLSCAFile)
		}
		cfg.RootCAs = pool
	}
	if c.TLSServerName != "" {
		cfg.ServerName = c.TLSServerName
	}
	return cfg, nil
}

// certificateNodeID is the node name the configured client certificate
// carries — its Common Name — or "" when no certificate file is configured.
// It is what the agent registers under when NodeIDOverride is empty, so an
// admin server that binds identity to the certificate
// (--agent-identity-from-cert) sees the two agree without the operator
// writing the name twice.
func (c ExtensionConfig) certificateNodeID() (string, error) {
	if c.TLSCertFile == "" {
		return "", nil
	}
	cert, err := loadClientCertificate(c.TLSCertFile, c.TLSKeyFile)
	if err != nil {
		return "", err
	}
	cn := strings.TrimSpace(cert.Leaf.Subject.CommonName)
	if cn == "" {
		return "", fmt.Errorf("admin agent: client certificate %q has no Common Name to name the node; set node_id", c.TLSCertFile)
	}
	return cn, nil
}

func (c ExtensionConfig) hasTLSFiles() bool {
	return c.TLSCertFile != "" || c.TLSKeyFile != "" || c.TLSCAFile != "" || c.TLSServerName != ""
}

// loadClientCertificate reads a PEM certificate/key pair and guarantees the
// parsed leaf is attached, whatever the Go version's LoadX509KeyPair does.
func loadClientCertificate(certFile, keyFile string) (tls.Certificate, error) {
	if keyFile == "" {
		return tls.Certificate{}, errors.New("admin agent: tls_cert_file and tls_key_file must be set together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("admin agent: load client certificate: %w", err)
	}
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("admin agent: parse client certificate %q: %w", certFile, err)
		}
		cert.Leaf = leaf
	}
	return cert, nil
}
