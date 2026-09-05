package admin

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The SPA is committed as internal/admin/ui/dist and embedded at build time;
// these tests look at what the binary actually ships, so a chunk the build
// emitted but the embed or the router missed shows up here, not in a
// browser. Since the feature pages are lazy (ui/src/routes.ts), the assets
// index.html references on its own are the initial load — everything else
// arrives on navigation.

// Budgets for the assets index.html loads before any navigation. Before
// route-level code splitting the entry pulled 1,693 KB of JS and 252 KB of
// CSS (the AG Grid styles included) on every page, the login screen too;
// after it the same load is ~300 KB of JS and ~29 KB of CSS. The limits leave
// room for dependency bumps but not for a feature page sneaking back into
// the entry.
const (
	initialJSBudget  = 512 * 1024
	initialCSSBudget = 64 * 1024
)

var indexAssetRef = regexp.MustCompile(`(?:src|href)="\./assets/([^"]+)"`)

// embeddedDist returns the SPA embedded in the binary. The placeholder
// fallback has no assets/, so a tree built without the real dist fails here
// instead of passing vacuously.
func embeddedDist(t *testing.T) fs.FS {
	t.Helper()
	dist, err := fs.Sub(builtUIFS, "ui/dist")
	if err != nil {
		t.Fatalf("embedded dist: %v", err)
	}
	if !adminUIFSHasIndex(dist) {
		t.Fatal("embedded dist has no index.html; build internal/admin/ui first")
	}
	if _, err := fs.Stat(dist, "assets"); err != nil {
		t.Fatalf("embedded dist has no assets/: %v", err)
	}
	return dist
}

// initialAssets lists the ./assets/* files index.html references directly
// (module script, modulepreload links and stylesheets).
func initialAssets(t *testing.T, dist fs.FS) []string {
	t.Helper()
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	var names []string
	for _, m := range indexAssetRef.FindAllStringSubmatch(string(index), -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("index.html references no ./assets/*: %s", index)
	}
	return names
}

func TestEmbeddedDist_IndexReferencesExistingAssets(t *testing.T) {
	dist := embeddedDist(t)
	for _, name := range initialAssets(t, dist) {
		if _, err := fs.Stat(dist, path.Join("assets", name)); err != nil {
			t.Errorf("index.html references assets/%s but the embedded dist has no such file: %v", name, err)
		}
	}
}

func TestEmbeddedDist_InitialLoadStaysWithinBudget(t *testing.T) {
	dist := embeddedDist(t)
	var js, css int64
	for _, name := range initialAssets(t, dist) {
		info, err := fs.Stat(dist, path.Join("assets", name))
		if err != nil {
			t.Fatalf("stat assets/%s: %v", name, err)
		}
		switch path.Ext(name) {
		case ".js":
			js += info.Size()
		case ".css":
			css += info.Size()
		}
	}
	if js > initialJSBudget {
		t.Errorf("initial JS load is %d bytes, budget %d — a feature page is probably imported statically again (see ui/src/routes.ts)", js, initialJSBudget)
	}
	if css > initialCSSBudget {
		t.Errorf("initial CSS load is %d bytes, budget %d — the AG Grid styles are back in the entry stylesheet", css, initialCSSBudget)
	}
}

// The shipped grid stylesheet carries the light and dark quartz variants the
// panel toggles between; the auto-dark one (prefers-color-scheme) is dropped
// at build time by ui/tools/postcss-strip-unused-grid-themes.ts.
func TestEmbeddedDist_DropsUnusedGridThemeVariant(t *testing.T) {
	dist := embeddedDist(t)
	var sawGridTheme bool
	err := fs.WalkDir(dist, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".css" {
			return err
		}
		content, err := fs.ReadFile(dist, p)
		if err != nil {
			return err
		}
		css := string(content)
		if strings.Contains(css, ".ag-theme-quartz-dark") {
			sawGridTheme = true
		}
		if strings.Contains(css, "ag-theme-quartz-auto-dark") {
			t.Errorf("%s still carries the ag-theme-quartz-auto-dark variant the panel never applies", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk assets: %v", err)
	}
	if !sawGridTheme {
		t.Fatal("no embedded stylesheet carries the quartz dark theme; the Data Studio chunk is missing its CSS")
	}
}

// Every file the build emitted under assets/ is served under a custom mount
// prefix with a content type the browser accepts for a module script or a
// stylesheet — the lazy chunks resolve their URLs relative to the entry
// module, so a prefix other than /admin must work for them as it does for
// the entry.
func TestPanel_EmbeddedDistAssetsServed(t *testing.T) {
	t.Setenv(adminUIDirEnv, "") // make sure the on-disk override is off
	dist := embeddedDist(t)

	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.config.Prefix = "/nucleus-admin"

	root := router.NewMux()
	root.Mount("/nucleus-admin", panel.Handler())
	srv := httptest.NewServer(root)
	defer srv.Close()

	var served int
	err := fs.WalkDir(dist, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		res, err := http.Get(srv.URL + "/nucleus-admin/" + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET /nucleus-admin/%s: status=%d body=%q", p, res.StatusCode, truncate(body, 120))
			return nil
		}
		ct := res.Header.Get("Content-Type")
		switch path.Ext(p) {
		case ".js":
			if !strings.HasPrefix(ct, "text/javascript") && !strings.HasPrefix(ct, "application/javascript") {
				t.Errorf("GET /nucleus-admin/%s: Content-Type %q is not a JavaScript type", p, ct)
			}
		case ".css":
			if !strings.HasPrefix(ct, "text/css") {
				t.Errorf("GET /nucleus-admin/%s: Content-Type %q is not text/css", p, ct)
			}
		}
		if info, err := d.Info(); err == nil && int64(len(body)) != info.Size() {
			t.Errorf("GET /nucleus-admin/%s: served %d bytes, embedded file is %d", p, len(body), info.Size())
		}
		served++
		return nil
	})
	if err != nil {
		t.Fatalf("walk assets: %v", err)
	}
	if served < 2 {
		t.Fatalf("expected the embedded dist to ship several assets, served %d", served)
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
