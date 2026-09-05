package fleettest

import (
	"io"
	"log/slog"
	"net/http"
	"os"
)

// The helpers below mirror the ones server's own tests keep; the two test
// suites are separate modules on purpose (ADR-006), so they cannot share them.

func discardLogger() *slog.Logger {
	if os.Getenv("ADMIN_SERVER_DEBUG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// uiH2CClient talks to the UI listener as a trusted operator: the server
// trusts X-Auth-* headers from loopback (ADR-004).
func uiH2CClient() *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport: &headerInjector{
			next:    base,
			headers: map[string]string{"X-Auth-User": "test-operator"},
		},
	}
}

type headerInjector struct {
	next    http.RoundTripper
	headers map[string]string
}

func (h *headerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.next.RoundTrip(req)
}
