package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/datasource"
)

// singleAliasSource serves exactly one database alias, the way
// quarkdatasource does: asking it for a store on any other alias is an error,
// not a silent answer from the wrong database.
type singleAliasSource struct {
	alias  string
	infos  []datasource.ModelInfo
	counts map[string]int64
	fail   map[string]error // model → error on the served alias too
}

func (s *singleAliasSource) All() []datasource.ModelInfo { return s.infos }

func (s *singleAliasSource) Get(name string) (datasource.ModelInfo, bool) {
	for _, m := range s.infos {
		if m.Name == name {
			return m, true
		}
	}
	return datasource.ModelInfo{}, false
}

func (s *singleAliasSource) Store(modelName, dbAlias string) (datasource.RecordStore, error) {
	if dbAlias != "" && dbAlias != s.alias {
		return nil, errors.New("singleAliasSource: alias " + dbAlias + " not served")
	}
	if err, ok := s.fail[modelName]; ok {
		return nil, err
	}
	return fixedCountStore{count: s.counts[modelName]}, nil
}

func newTwoAliasPanel(t *testing.T, src datasource.DataSource) *httptest.Server {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	handles := map[string]*db.DB{}
	for _, alias := range []string{"default", "audit"} {
		database, err := db.New(db.Config{
			Engine:          db.EngineSQL,
			DatabaseURL:     "sqlite://:memory:",
			DatabaseMaxOpen: 1,
			DatabaseMaxIdle: 1,
		}, logger)
		if err != nil {
			t.Fatalf("db.New(%s) failed: %v", alias, err)
		}
		t.Cleanup(func() { _ = database.Close() })
		handles[alias] = database
	}
	panel := NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin"}},
		DatabaseHandles: handles,
	})
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// TestHandleListModels_ForeignAliasIsAbsenceNotError: the presence sweep of
// ListModels asks every alias the app serves. A data source bound to one
// alias answers "not served" for the others — that is the model being absent
// there, exactly like a missing table, and must not turn the whole listing
// into a 500. Caught by the reference consumer (quantum-app: Quark models on
// "default", an "audit" MySQL alias for its own module) against orbit
// v1.8.18, where quarkdatasource started refusing foreign aliases.
func TestHandleListModels_ForeignAliasIsAbsenceNotError(t *testing.T) {
	src := &singleAliasSource{
		alias:  "default",
		infos:  []datasource.ModelInfo{{Name: "Product", Plural: "Products", Table: "products"}},
		counts: map[string]int64{"Product": 3},
	}
	srv := newTwoAliasPanel(t, src)

	for _, q := range []string{"", "?include_counts=true"} {
		res, err := http.Get(srv.URL + "/api/models" + q)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		var payload struct {
			Models []struct {
				Name      string   `json:"name"`
				Count     int64    `json:"count"`
				Databases []string `json:"databases"`
			} `json:"models"`
		}
		err = json.NewDecoder(res.Body).Decode(&payload)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%q: expected 200, got %d", q, res.StatusCode)
		}
		if err != nil {
			t.Fatalf("%q: decode failed: %v", q, err)
		}
		if len(payload.Models) != 1 || payload.Models[0].Name != "Product" {
			t.Fatalf("%q: expected the single Product model, got %+v", q, payload.Models)
		}
		if dbs := payload.Models[0].Databases; len(dbs) != 1 || dbs[0] != "default" {
			t.Fatalf("%q: Product must be attributed to its own alias only, got %v", q, dbs)
		}
		if q != "" && payload.Models[0].Count != 3 {
			t.Fatalf("count lost on the served alias: got %d, want 3", payload.Models[0].Count)
		}
	}
}

// An error on the model's OWN alias is still an error: the panel cannot
// explain that one away as absence.
func TestHandleListModels_OwnAliasErrorStillFails(t *testing.T) {
	src := &singleAliasSource{
		alias:  "default",
		infos:  []datasource.ModelInfo{{Name: "Product", Plural: "Products", Table: "products"}},
		counts: map[string]int64{"Product": 3},
		fail:   map[string]error{"Product": errors.New("boom")},
	}
	srv := newTwoAliasPanel(t, src)
	res, err := http.Get(srv.URL + "/api/models?include_counts=true")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		t.Fatalf("an error on the served alias must not be swallowed; got 200")
	}
}
