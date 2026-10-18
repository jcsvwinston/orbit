// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package orbit

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit/datasource"
)

// emptySource is a data source the application hands over: nothing to
// browse, which is enough for the panel to mount on it.
type emptySource struct{}

func (emptySource) All() []datasource.ModelInfo { return nil }

func (emptySource) Get(string) (datasource.ModelInfo, bool) { return datasource.ModelInfo{}, false }

func (emptySource) Store(string, string) (datasource.RecordStore, error) {
	return nil, errors.New("empty source: no models")
}

func mountConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "orbit.db") + "?_pragma=busy_timeout(10000)"},
	}
	cfg.JWTSecret = strings.Repeat("datasource-required-secret", 2)
	cfg.Storage.Local.Path = t.TempDir()
	return cfg
}

// freePort reserves a loopback port for an application the test boots by
// hand (nucleustest.StartApp fails the test on a boot error, and a boot
// error is what the first case expects).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// Config.DataSource is required (DEP-2026-002): the one default there was,
// the Nucleus adapter over the application's pkg/model registry, left with
// the registry it read. An application that mounts the panel without a
// source does not start, and the error names the field; one that hands the
// panel its own source mounts as before.
func TestModule_DataSourceIsRequired(t *testing.T) {
	t.Run("nil_refuses_to_start_naming_the_field", func(t *testing.T) {
		cfg := mountConfig(t)
		cfg.Host = "127.0.0.1"
		cfg.Port = freePort(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := nucleus.RunContext(ctx, nucleus.App{
			Config:  cfg,
			Modules: map[string]nucleus.ModuleSpec{"orbit": Module(Config{Prefix: "/admin"})},
		})
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("an application that mounts the panel with no DataSource started: err=%v", err)
		}
		for _, want := range []string{"Config.DataSource", "required"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the start error lacks %q: %v", want, err)
			}
		}
	})

	t.Run("own_data_source_mounts", func(t *testing.T) {
		srv := nucleustest.StartApp(t, nucleus.App{
			Config:  mountConfig(t),
			Modules: map[string]nucleus.ModuleSpec{"orbit": Module(Config{Prefix: "/admin", DataSource: emptySource{}})},
		})
		if res := srv.Get("/admin/api/health"); res.Status >= 500 {
			t.Fatalf("the panel on its own source does not answer: %d %s", res.Status, res)
		}
	})
}
