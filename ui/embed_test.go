package ui

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// Budgets for the assets each entry's index.html loads before any
// navigation, raw bytes as embedded. The panel's are the ones it has kept
// since its feature pages became dynamic imports; the fleet's is its whole
// bundle today, one chunk, and the number is a ceiling for the re-skin to
// respect, not a target.
const (
	// The panel's entry.
	initialJSBudget  = 512 * 1024
	initialCSSBudget = 64 * 1024
	// The fleet's entry.
	fleetInitialJSBudget  = 512 * 1024
	fleetInitialCSSBudget = 64 * 1024
)

var indexAssetRef = regexp.MustCompile(`(?:src|href)="\./assets/([^"]+)"`)

func entryFS(t *testing.T, name string, get func() fs.FS) fs.FS {
	t.Helper()
	dist := get()
	if dist == nil {
		t.Fatalf("embedded dist has no %s/index.html; run npm ci && npm run build in ui/", name)
	}
	if _, err := fs.Stat(dist, "assets"); err != nil {
		t.Fatalf("embedded %s has no assets/: %v", name, err)
	}
	return dist
}

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

func initialLoad(t *testing.T, dist fs.FS) (js, css int64) {
	t.Helper()
	for _, name := range initialAssets(t, dist) {
		info, err := fs.Stat(dist, path.Join("assets", name))
		if err != nil {
			t.Fatalf("index.html references assets/%s, which the dist does not carry: %v", name, err)
		}
		switch path.Ext(name) {
		case ".js":
			js += info.Size()
		case ".css":
			css += info.Size()
		}
	}
	return js, css
}

// Both entries are there, reference assets the dist carries, and load
// within their budgets.
func TestEmbeddedDist_BothEntriesWithinBudget(t *testing.T) {
	panel := entryFS(t, "panel", Panel)
	js, css := initialLoad(t, panel)
	if js > initialJSBudget {
		t.Errorf("panel initial JS is %d bytes, budget %d — a feature page is probably imported statically again (see src/routes.ts)", js, initialJSBudget)
	}
	if css > initialCSSBudget {
		t.Errorf("panel initial CSS is %d bytes, budget %d — the AG Grid styles are back in the entry stylesheet", css, initialCSSBudget)
	}
	fleet := entryFS(t, "fleet", Fleet)
	js, css = initialLoad(t, fleet)
	if js > fleetInitialJSBudget {
		t.Errorf("fleet initial JS is %d bytes, budget %d", js, fleetInitialJSBudget)
	}
	if css > fleetInitialCSSBudget {
		t.Errorf("fleet initial CSS is %d bytes, budget %d", css, fleetInitialCSSBudget)
	}
	if Dist() == nil {
		t.Fatal("Dist() must expose the whole tree")
	}
}

// The panel's Data Studio chunk carries the quartz dark grid theme and
// nothing carries the auto-dark variant the panel never applies (the
// PostCSS pass in tools/ strips it).
func TestEmbeddedDist_PanelDropsUnusedGridThemeVariant(t *testing.T) {
	dist := entryFS(t, "panel", Panel)
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
		t.Fatal("no panel stylesheet carries the quartz dark theme; the Data Studio chunk is missing its CSS")
	}
}
