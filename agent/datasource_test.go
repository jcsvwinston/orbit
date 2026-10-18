// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// quarkLikeSource stands in for a data source the application hands over
// (a quarkdatasource adapter in practice): one model, nothing to browse.
type quarkLikeSource struct{ datasource.DataSource }

func (quarkLikeSource) All() []datasource.ModelInfo {
	return []datasource.ModelInfo{{Name: "QuarkNote", Table: "quark_notes"}}
}

func newAgentForTest(t *testing.T, cfg Config) (*Agent, string) {
	t.Helper()
	var logs bytes.Buffer
	cfg.Endpoints = []string{"http://127.0.0.1:1"}
	cfg.StateDir = t.TempDir()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ag, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag, logs.String()
}

// Data Studio on a fleet node is what the application hands the agent
// (ADR-002): a node started without a DataSource serves none — it
// registers no models and starts without a word, since there is no default
// in play (the Nucleus adapter over the node's pkg/model registry left at
// 2.0, DEP-2026-002) — and a node with its own source serves that source.
func TestNew_DataSourceIsWhatTheApplicationHandsOver(t *testing.T) {
	t.Run("no_data_source_no_data_studio", func(t *testing.T) {
		ag, logs := newAgentForTest(t, Config{})
		if ag.dataStudio != nil {
			t.Fatal("a node with no DataSource built a Data Studio handler")
		}
		if models := ag.dataStudio.RegisteredModels(); len(models) != 0 {
			t.Fatalf("a node with no DataSource registers models: %v", models)
		}
		if strings.Contains(logs, "DEP-2026-002") || strings.Contains(logs, "Nucleus adapter") {
			t.Fatalf("a node with no DataSource was warned about a default that no longer exists; log was:\n%s", logs)
		}
	})

	t.Run("own_data_source", func(t *testing.T) {
		ag, logs := newAgentForTest(t, Config{DataSource: quarkLikeSource{}})
		if ag.dataStudio == nil {
			t.Fatal("a node with its own DataSource serves no Data Studio")
		}
		if models := ag.dataStudio.RegisteredModels(); len(models) != 1 || models[0] != "QuarkNote" {
			t.Fatalf("the node registers the source's models, got %v", models)
		}
		if strings.Contains(logs, "DEP-2026-002") {
			t.Fatalf("a node on its own data source was warned about the default; log was:\n%s", logs)
		}
	})
}
