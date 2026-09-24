package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/orbit/server/alerts"
)

// A rule that names a channel nobody configured, or a webhook with a
// non-http URL, is a configuration error: Run refuses to start rather
// than evaluating a rule that would notify nobody.
func TestRun_RefusesMisconfiguredAlerts(t *testing.T) {
	cases := map[string]Config{
		"unknown channel": {AgentAddr: "127.0.0.1:0", UIAddr: "127.0.0.1:0",
			AlertRules: []alerts.Rule{{Name: "x", Metric: "cpu_percent", Op: ">", Threshold: 1, Channels: []string{"nobody"}}}},
		"bad webhook": {AgentAddr: "127.0.0.1:0", UIAddr: "127.0.0.1:0",
			AlertWebhooks: map[string]string{"ops": "ftp://nope"}},
		"bad metric": {AgentAddr: "127.0.0.1:0", UIAddr: "127.0.0.1:0",
			AlertRules: []alerts.Rule{{Name: "x", Metric: "load", Op: ">", Threshold: 1}}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := New(cfg).Run(ctx)
			if err == nil || !strings.Contains(err.Error(), "alerts:") {
				t.Fatalf("Run must refuse with the alerts error, got %v", err)
			}
		})
	}
}
