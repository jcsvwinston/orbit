// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
)

// bootstrapPassword is what the bench signs in with. It is the password the
// mounted module creates the admin user with, the same way an application
// sets ADMIN_BOOTSTRAP_PASSWORD.
const bootstrapPassword = "adminbench-bootstrap-password"

// Note is the one domain model the probed application registers. Orbit reads
// the HOST application's model registry, so a panel mounted on an app with no
// models is an empty panel — every Data Studio probe needs something to
// browse. The tags are the ones an application author writes: `validate` for
// the constraint, `admin` for what the panel does with the field.
type Note struct {
	model.BaseModel

	Title  string `db:"required" json:"title" validate:"required" admin:"list,search"`
	Body   string `json:"body"`
	Status string `json:"status" admin:"list,filter"`
	Views  int    `json:"views" admin:"list"`
}

// Author exists only so a foreign key does: Note has no relation, and a bench
// that never registers two related models cannot measure what the panel does
// with relations.
type Author struct {
	model.BaseModel

	Name string `db:"required" json:"name" admin:"list,search"`
}

// Credential exists for the redaction probe: a model whose field names say
// "secret" the way a real one does.
type Credential struct {
	model.BaseModel

	Label    string `db:"required" json:"label" admin:"list,search"`
	Password string `json:"password"`
	APIToken string `json:"api_token" db:"column:api_token"`
}

// Comment points at both, which is what makes it the relation probe's model.
// The foreign keys are declared the way the framework documents them
// (`fk:model=…`). The bench learned this the hard way: with only a NoteID
// field and no declaration, nothing marked it as a key and the first run read
// that as "the panel has no relation metadata". It was measuring the model
// the bench wrote, not the product.
type Comment struct {
	model.BaseModel

	NoteID   uint   `json:"note_id" db:"column:note_id;fk:model=Note,table=notes,column=id"`
	AuthorID uint   `json:"author_id" db:"column:author_id;fk:model=Author,table=authors,column=id"`
	Body     string `json:"body"`
}

func contentModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   "content",
		Models: []any{Note{}, Author{}, Comment{}, Credential{}},
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			return rt.AutoMigrate(Note{}, Author{}, Comment{}, Credential{})
		},
	}.Build()
}

// env is one booted application with the panel mounted, shared by every
// probe and torn down by the parent test.
//
// The application is what an author WRITES: a default config, one module with
// models, and orbit.Module with the four fields the quick start uses. No
// probe reaches inside the panel to wire a capability into place — a control
// that only works because the bench arranged it measures the bench.
type env struct {
	tb   testing.TB
	once sync.Once
	srv  *nucleustest.Server
	jar  *http.Client

	operators map[string]*operator

	migrations       *nucleustest.Server
	migrationsClient *http.Client

	// dbs is the database the probed application opened, kept so a probe can
	// boot a SECOND application on the same storage — the only honest way to
	// ask whether something the panel holds survives the process.
	dbs map[string]app.DatabaseConfig
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func (e *env) server() *nucleustest.Server {
	e.once.Do(func() {
		cfg := benchConfig(e.tb)
		e.dbs = cfg.Databases

		e.srv = nucleustest.StartApp(e.tb, nucleus.App{
			Config: cfg,
			Modules: map[string]nucleus.ModuleSpec{
				"content": contentModule(),
				"orbit": orbit.Module(orbit.Config{
					Prefix:            "/admin",
					Title:             "Admin Bench",
					BootstrapUsername: "admin",
					BootstrapEmail:    "admin@example.test",
					BootstrapPassword: bootstrapPassword,
				}),
			},
		})
	})
	return e.srv
}

// operator returns a client signed in as the bootstrap admin — the panel as
// an operator holds it, cookies and all.
func (e *env) operator(t *testing.T) *http.Client {
	t.Helper()
	if e.jar != nil {
		return e.jar
	}
	srv := e.server()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	form := url.Values{"username": {"admin"}, "password": {bootstrapPassword}}
	resp, err := client.PostForm(srv.URL("/admin/login"), form)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("sign in answered %d: %s", resp.StatusCode, truncate(string(body)))
	}
	e.jar = client
	return client
}

type response struct {
	code  int
	ctype string
	body  []byte
}

// servedTheShell reports whether this answer is the single-page application's
// index document rather than an API answer. The panel mounts a catch-all that
// serves the built UI for any unmatched path UNDER the whole prefix, /api
// included, so "there is no such endpoint" arrives as 200 text/html. Every
// probe that measures an absence has to see through that, or it measures the
// fallback.
func (r response) servedTheShell() bool {
	if strings.Contains(r.ctype, "text/html") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(string(r.body)))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode body (%d): %v: %s", r.code, err, truncate(string(r.body)))
	}
	return out
}

// text is for LOGS: it truncates. raw is for MEASUREMENT: a probe that asks
// whether a payload contains something has to see all of it — a truncated
// body answered "no" for three controls in this bench's first run.
func (r response) text() string { return truncate(string(r.body)) }

func (r response) raw() string { return string(r.body) }

// do issues one request as the signed-in bootstrap admin.
func (e *env) do(t *testing.T, method, path string, payload any) response {
	t.Helper()
	return e.request(t, e.operator(t), method, path, payload)
}

// request issues one request with the client it is given.
func (e *env) request(t *testing.T, client *http.Client, method, path string, payload any) response {
	t.Helper()
	srv := e.server()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL(path), body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{code: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), body: raw}
}

func (e *env) get(t *testing.T, path string) response {
	return e.do(t, http.MethodGet, path, nil)
}

// unrouted turns "the panel serves nothing there" into a verdict. Any answer
// other than 404 means a surface exists and the control needs a real probe
// instead of this one.
func (e *env) unrouted(t *testing.T, paths ...string) verdict {
	t.Helper()
	for _, p := range paths {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			r := e.do(t, m, p, map[string]any{})
			// 404 is the honest answer; 200 text/html is the single-page
			// fallback catching the path; 405 is the router matching only
			// the fallback's GET pattern. None of the three is an endpoint.
			if r.code == http.StatusNotFound || r.code == http.StatusMethodNotAllowed || r.servedTheShell() {
				continue
			}
			t.Logf("%s %s answered %d (%s), not 404: the surface exists", m, p, r.code, r.ctype)
			return partial
		}
	}
	return absent
}

// createNote posts one record and returns its id, failing the probe when the
// panel refuses — every Data Studio probe that needs a row starts here.
func (e *env) createNote(t *testing.T, fields map[string]any) string {
	t.Helper()
	r := e.do(t, http.MethodPost, "/admin/api/models/Note", fields)
	if r.code != http.StatusCreated && r.code != http.StatusOK {
		t.Fatalf("create Note answered %d: %s", r.code, r.text())
	}
	return recordID(t, r.json(t))
}

func recordID(t *testing.T, payload map[string]any) string {
	t.Helper()
	data := payload
	if inner, ok := payload["data"].(map[string]any); ok {
		data = inner
	}
	switch id := data["id"].(type) {
	case string:
		return id
	case float64:
		return fmt.Sprintf("%d", int64(id))
	}
	t.Fatalf("no id in response: %v", payload)
	return ""
}

func truncate(s string) string {
	const max = 400
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// decodeInto reads a JSON body into out, failing the probe if the panel
// answered something else.
func decodeInto(t *testing.T, r io.Reader, out any) {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode body: %v: %s", err, truncate(string(raw)))
	}
}

// benchConfig is the application configuration every probe boots from: a
// default one, with a temporary database and a temporary storage root so a
// run leaves nothing behind in the package directory (imports, exports and
// fixtures all write through the local storage provider, which is relative to
// the working directory by default).
func benchConfig(tb testing.TB) app.Config {
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(tb)
	cfg.JWTSecret = strings.Repeat("adminbench-probe-secret", 2)
	cfg.Storage.Local.Path = tb.TempDir()
	return cfg
}
