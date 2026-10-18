// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package orbit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit/datasource"
)

// emptySource is a data source the application hands over: nothing to
// browse, which is enough for the panel to mount on it and for the test to
// tell "the application chose" from "the application is on the default".
type emptySource struct{}

func (emptySource) All() []datasource.ModelInfo { return nil }

func (emptySource) Get(string) (datasource.ModelInfo, bool) { return datasource.ModelInfo{}, false }

func (emptySource) Store(string, string) (datasource.RecordStore, error) {
	return nil, errors.New("empty source: no models")
}

// captureStdout runs fn with os.Stdout redirected into a pipe and returns
// what was written. The application's logger is built on os.Stdout
// (observe.NewLogger has no writer option), so the WARN the mount writes
// lands there and nowhere a test could otherwise read it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	out := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	return <-out
}

func mountConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "orbit.db") + "?_pragma=busy_timeout(10000)"},
	}
	cfg.JWTSecret = strings.Repeat("datasource-deprecation-secret", 2)
	cfg.Storage.Local.Path = t.TempDir()
	return cfg
}

// The default data source — Data Studio over the application's pkg/model
// registry — leaves at Orbit 2.0 (DEP-2026-002). An application still on it
// is told once, at mount; one that hands the panel its own source hears
// nothing, because no default is in play.
func TestModule_DefaultDataSourceIsAnnouncedAsLeaving(t *testing.T) {
	mount := func(t *testing.T, cfg Config) string {
		t.Helper()
		cfg.Prefix = "/admin"
		return captureStdout(t, func() {
			nucleustest.StartApp(t, nucleus.App{
				Config:  mountConfig(t),
				Modules: map[string]nucleus.ModuleSpec{"orbit": Module(cfg)},
			})
		})
	}

	t.Run("default_over_the_registry", func(t *testing.T) {
		got := mount(t, Config{})
		for _, want := range []string{"DEP-2026-002", "Nucleus adapter", "orbit.Config.DataSource", "quarkdatasource"} {
			if !strings.Contains(got, want) {
				t.Errorf("the mount WARN lacks %q; stdout was:\n%s", want, got)
			}
		}
	})

	t.Run("own_data_source", func(t *testing.T) {
		if got := mount(t, Config{DataSource: emptySource{}}); strings.Contains(got, "DEP-2026-002") {
			t.Fatalf("a panel on its own data source was warned about the default; stdout was:\n%s", got)
		}
	})
}
