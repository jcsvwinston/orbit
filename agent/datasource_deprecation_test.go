// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"

	"github.com/jcsvwinston/orbit/datasource"
)

// quarkLikeSource stands in for a data source the application hands over
// (a quarkdatasource adapter in practice): the agent must not warn about
// the default when there is no default in use.
type quarkLikeSource struct{ datasource.DataSource }

// The default data source — Data Studio over the Nucleus model registry —
// leaves at Orbit 2.0 (DEP-2026-002). A node still on it is told once, at
// start, and a node that hands the agent its own data source, or has no
// registry to browse, hears nothing: there is no default in play.
func TestNew_DefaultDataSourceIsAnnouncedAsLeaving(t *testing.T) {
	newAgent := func(t *testing.T, cfg Config) string {
		t.Helper()
		var logs bytes.Buffer
		cfg.Endpoints = []string{"http://127.0.0.1:1"}
		cfg.StateDir = t.TempDir()
		cfg.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
		if _, err := New(cfg); err != nil {
			t.Fatalf("New: %v", err)
		}
		return logs.String()
	}

	t.Run("default_over_the_registry", func(t *testing.T) {
		got := newAgent(t, Config{Registry: model.NewRegistry()})
		for _, want := range []string{"DEP-2026-002", "Nucleus adapter", "agent.Config.DataSource", "quarkdatasource"} {
			if !strings.Contains(got, want) {
				t.Errorf("the startup WARN lacks %q; log was:\n%s", want, got)
			}
		}
	})

	t.Run("own_data_source", func(t *testing.T) {
		if got := newAgent(t, Config{Registry: model.NewRegistry(), DataSource: quarkLikeSource{}}); strings.Contains(got, "DEP-2026-002") {
			t.Fatalf("a node on its own data source was warned about the default; log was:\n%s", got)
		}
	})

	t.Run("no_registry_no_data_studio", func(t *testing.T) {
		if got := newAgent(t, Config{}); strings.Contains(got, "DEP-2026-002") {
			t.Fatalf("a node with no registry was warned about a default it does not use; log was:\n%s", got)
		}
	})
}
