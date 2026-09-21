package agent

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

// keyPairFiles serves the client certificate at certFile/keyFile — and keeps
// serving the pair the files hold as they change. Every handshake asks the
// files whether they moved (size or modification time); when they did, the
// pair is re-read and the connection being opened presents the new
// certificate. A rotation is a write to the two files; the agent picks it
// up on its next connection, which under a live stream is its next
// reconnect. No restart, no signal, no watcher.
//
// The first load must succeed. A later one that fails — a rotation half
// written, a key that does not match — keeps the previous certificate and
// logs one WARN per distinct error, then tries again next handshake. The
// same type lives in the server module: ADR-006 allows no module the two
// could share.
type keyPairFiles struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	stamp   fileStamp
	lastErr string
}

type fileStamp struct {
	certSize, keySize int64
	certMod, keyMod   time.Time
}

func newKeyPairFiles(certFile, keyFile string, logger *slog.Logger) (*keyPairFiles, error) {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return nil, errors.New("admin agent: tls_cert_file and tls_key_file must be set together")
	}
	if logger == nil {
		logger = slog.Default()
	}
	k := &keyPairFiles{certFile: certFile, keyFile: keyFile, logger: logger}
	stamp, err := stampFiles(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("admin agent: client certificate files: %w", err)
	}
	cert, err := loadPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	k.cert, k.stamp = &cert, stamp
	return k, nil
}

// current returns the certificate the files hold now.
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
	cert, err := loadPair(k.certFile, k.keyFile)
	if err != nil {
		k.warnOnce(err.Error())
		return k.cert
	}
	k.cert, k.stamp, k.lastErr = &cert, stamp, ""
	k.logger.Info("admin agent client certificate reloaded from files",
		"tls_cert_file", k.certFile, "common_name", cert.Leaf.Subject.CommonName)
	return k.cert
}

func (k *keyPairFiles) warnOnce(msg string) {
	if msg == k.lastErr {
		return
	}
	k.lastErr = msg
	k.logger.Warn("admin agent client certificate files changed but could not be loaded; keeping the previous certificate",
		"tls_cert_file", k.certFile, "tls_key_file", k.keyFile, "error", msg)
}

// loadPair reads a PEM certificate/key pair and guarantees the parsed leaf
// is attached, whatever the Go version's LoadX509KeyPair does.
func loadPair(certFile, keyFile string) (tls.Certificate, error) {
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
