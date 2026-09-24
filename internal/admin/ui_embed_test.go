package admin

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/router"

	orbitui "github.com/jcsvwinston/orbit/ui"
)

// The SPA is the panel entry of ui/dist, embedded by the ui module (ADR-015);
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
// The panel's dist budgets and shape are the ui module's tests
// (ui/embed_test.go, ADR-015); here only that the panel serves what the
// module embeds.
func embeddedDist(t *testing.T) fs.FS {
	t.Helper()
	dist := orbitui.Panel()
	if dist == nil {
		t.Fatal("the ui module embeds no panel entry; build ui/ first")
	}
	return dist
}

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
