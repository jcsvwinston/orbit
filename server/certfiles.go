package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// TLSFromFiles builds a server-side TLS configuration whose certificate is
// the PEM pair at certFile/keyFile — and stays that pair as the files
// change. Every handshake asks the files whether they moved (size or
// modification time); when they did, the pair is re-read and the new
// certificate is what that handshake presents. No restart, no signal, no
// watcher: a rotation is a write to the two files.
//
// The first load must succeed, or the configuration is refused. A later
// load that fails — a rotation half written, a key that does not match the
// new certificate — keeps the previous certificate serving and logs one
// WARN per distinct error, then tries again on the next handshake. Add
// ClientCAs/ClientAuth to the result for mutual TLS; those are not
// reloaded here.
func TLSFromFiles(certFile, keyFile string, logger *slog.Logger) (*tls.Config, error) {
	src, err := newKeyPairFiles(certFile, keyFile, logger)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return src.current(), nil },
	}, nil
}

// ErrNotAClientCertificate reports a certificate whose extended key usage
// leaves out client authentication. A TLS server that verifies client
// certificates — a peer's agent listener under --agent-client-ca among
// them — refuses it in the handshake, so a server presenting it to its
// peers never joins their mesh.
var ErrNotAClientCertificate = errors.New("the certificate's extended key usage does not include clientAuth")

// PeerTLSFromFiles builds the client side of the links this server opens to
// its peers (Config.PeerTLS) from PEM files.
//
// caFile, when non-empty, is the bundle the peers' agent-listener
// certificates must chain to; empty keeps the system trust store. certFile
// and keyFile, given together or not at all, are the client certificate
// this server presents — what a peer whose agent listener verifies client
// certificates requires. Like TLSFromFiles, the pair is read from the files
// on every handshake that finds them changed, so a rotation needs no
// restart; the first load must succeed, and its certificate must allow
// client authentication (an extended key usage that includes clientAuth,
// or none at all), or the configuration is refused with an error wrapping
// ErrNotAClientCertificate. The CA bundle is read once.
func PeerTLSFromFiles(caFile, certFile, keyFile string, logger *slog.Logger) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(caFile) != "" {
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read the peer CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("peer CA bundle %q: no PEM certificates found", caFile)
		}
		cfg.RootCAs = pool
	}
	hasCert, hasKey := strings.TrimSpace(certFile) != "", strings.TrimSpace(keyFile) != ""
	if hasCert != hasKey {
		return nil, errors.New("a peer client certificate needs both the certificate and the key file")
	}
	if !hasCert {
		return cfg, nil
	}
	src, err := newKeyPairFiles(certFile, keyFile, logger)
	if err != nil {
		return nil, err
	}
	leaf := src.cert.Leaf
	if leaf == nil {
		if leaf, err = x509.ParseCertificate(src.cert.Certificate[0]); err != nil {
			return nil, fmt.Errorf("parse certificate %q: %w", certFile, err)
		}
	}
	if !allowsClientAuth(leaf) {
		return nil, fmt.Errorf("certificate %q: %w", certFile, ErrNotAClientCertificate)
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return src.current(), nil }
	return cfg, nil
}

// allowsClientAuth answers what crypto/x509 asks when a server verifies a
// client certificate: no extended key usage at all means any usage; a
// list must name clientAuth (or anyExtendedKeyUsage).
func allowsClientAuth(c *x509.Certificate) bool {
	if len(c.ExtKeyUsage) == 0 && len(c.UnknownExtKeyUsage) == 0 {
		return true
	}
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageClientAuth || u == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

// keyPairFiles is the reloading source behind TLSFromFiles. A copy of the
// same type lives in the agent module: ADR-006 allows no module the two
// could share.
type keyPairFiles struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	stamp   fileStamp
	lastErr string
}

// fileStamp is what a handshake compares to decide whether to re-read:
// cheap to take, and different for any write that changes bytes or time.
type fileStamp struct {
	certSize, keySize int64
	certMod, keyMod   time.Time
}

func newKeyPairFiles(certFile, keyFile string, logger *slog.Logger) (*keyPairFiles, error) {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return nil, errors.New("both the certificate and the key file must be given to enable TLS")
	}
	if logger == nil {
		logger = slog.Default()
	}
	k := &keyPairFiles{certFile: certFile, keyFile: keyFile, logger: logger}
	stamp, err := stampFiles(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load certificate %q with key %q: %w", certFile, keyFile, err)
	}
	k.cert, k.stamp = &cert, stamp
	return k, nil
}

// current returns the certificate the files hold now, re-reading them when
// their stamp changed since the last handshake.
func (k *keyPairFiles) current() *tls.Certificate {
	k.mu.Lock()
	defer k.mu.Unlock()
	stamp, err := stampFiles(k.certFile, k.keyFile)
	if err != nil {
		k.warnOnce(fmt.Sprintf("stat: %v", err))
		return k.cert
	}
	if stamp == k.stamp {
		return k.cert
	}
	cert, err := tls.LoadX509KeyPair(k.certFile, k.keyFile)
	if err != nil {
		// A rotation in progress, or a broken one: keep serving what
		// worked and look again on the next handshake.
		k.warnOnce(err.Error())
		return k.cert
	}
	k.cert, k.stamp, k.lastErr = &cert, stamp, ""
	k.logger.Info("tls certificate reloaded from files", "cert_file", k.certFile, "key_file", k.keyFile)
	return k.cert
}

func (k *keyPairFiles) warnOnce(msg string) {
	if msg == k.lastErr {
		return
	}
	k.lastErr = msg
	k.logger.Warn("tls certificate files changed but could not be loaded; keeping the previous certificate",
		"cert_file", k.certFile, "key_file", k.keyFile, "error", msg)
}

func stampFiles(certFile, keyFile string) (fileStamp, error) {
	ci, err := os.Stat(certFile)
	if err != nil {
		return fileStamp{}, err
	}
	ki, err := os.Stat(keyFile)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{certSize: ci.Size(), keySize: ki.Size(), certMod: ci.ModTime(), keyMod: ki.ModTime()}, nil
}
