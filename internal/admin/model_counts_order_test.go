package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/datasource"
)

// orderedCountSource is a minimal DataSource whose All() preserves the order
// it was built with — the shape quarkdatasource serves (registration order),
// as opposed to the nucleus adapter, whose registry sorts alphabetically and
// therefore masks ordering bugs.
type orderedCountSource struct {
	infos  []datasource.ModelInfo
	counts map[string]int64
}

func (s *orderedCountSource) All() []datasource.ModelInfo { return s.infos }

func (s *orderedCountSource) Get(name string) (datasource.ModelInfo, bool) {
	for _, m := range s.infos {
		if m.Name == name {
			return m, true
		}
	}
	return datasource.ModelInfo{}, false
}

func (s *orderedCountSource) Store(modelName, dbAlias string) (datasource.RecordStore, error) {
	return fixedCountStore{count: s.counts[modelName]}, nil
}

// fixedCountStore answers Count with a fixed per-model value; the rest of the
// RecordStore surface is unused by handleListModels.
type fixedCountStore struct{ count int64 }

func (f fixedCountStore) List(_ context.Context, _ datasource.Query) (datasource.Page, error) {
	return datasource.Page{}, nil
}

func (f fixedCountStore) Get(_ context.Context, _ string) (datasource.Record, error) {
	return datasource.Record{}, nil
}

func (f fixedCountStore) Create(_ context.Context, rec datasource.Record) (datasource.Record, error) {
	return rec, nil
}

func (f fixedCountStore) Update(_ context.Context, _ string, _ datasource.Record) error { return nil }

func (f fixedCountStore) Delete(_ context.Context, _ string) error { return nil }

func (f fixedCountStore) Count(_ context.Context) (datasource.CountResult, error) {
	return datasource.CountResult{Count: f.count, Present: true}, nil
}

func (f fixedCountStore) TableExists(_ context.Context) bool { return true }

// TestHandleListModels_CountsSurviveNonAlphabeticalRegistration is the
// regression guard for the swapped-counts bug: handleListModels took pointers
// into `result` BEFORE sort.SliceStable reordered it, so each pointer kept
// aiming at a position instead of a model, and with a non-alphabetical
// registration order every count landed on the wrong row (measured on the
// showcase demo: articles=7/authors=1 in the DB rendered as Article=1 and
// Author=7).
func TestHandleListModels_CountsSurviveNonAlphabeticalRegistration(t *testing.T) {
	// Registration order Author → Article: NOT alphabetical, with distinct
	// counts, so a position-based pointer swaps them.
	src := &orderedCountSource{
		infos: []datasource.ModelInfo{
			{Name: "Author", Plural: "Authors", Table: "authors"},
			{Name: "Article", Plural: "Articles", Table: "articles"},
		},
		counts: map[string]int64{"Author": 1, "Article": 7},
	}

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	panel := NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin"}},
		DatabaseHandles: map[string]*db.DB{"default": database},
	})

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/models")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Count int64  `json:"count"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	got := map[string]int64{}
	for _, m := range payload.Models {
		got[m.Name] = m.Count
	}
	if got["Article"] != 7 || got["Author"] != 1 {
		t.Fatalf("counts landed on the wrong models: got Article=%d Author=%d, want Article=7 Author=1", got["Article"], got["Author"])
	}
}
