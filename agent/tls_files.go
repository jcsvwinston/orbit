package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
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
// The client certificate is served from the files, not copied out of them:
// every handshake checks whether the two files changed and re-reads them
// when they did, so a rotated certificate is what the next connection
// presents — no restart. The CA bundle is read once. Rotate with the same
// Common Name when the server binds node identity to the certificate.
func (c ExtensionConfig) TLSConfig() (*tls.Config, error) {
	return c.tlsConfig(nil)
}

func (c ExtensionConfig) tlsConfig(logger *slog.Logger) (*tls.Config, error) {
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
		src, err := newKeyPairFiles(c.TLSCertFile, c.TLSKeyFile, logger)
		if err != nil {
			return nil, err
		}
		// The files replace whatever TLS carried: a deployment that names
		// its certificate in the configuration means that one. Go consults
		// GetClientCertificate before Certificates, so the latter is
		// cleared to leave no doubt about which one is presented.
		cfg.Certificates = nil
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return src.current(), nil }
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
	if c.TLSKeyFile == "" {
		return "", errors.New("admin agent: tls_cert_file and tls_key_file must be set together")
	}
	cert, err := loadPair(c.TLSCertFile, c.TLSKeyFile)
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
