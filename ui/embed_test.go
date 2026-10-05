package ui

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// compressedBudget is the most an operator may download, gzip-encoded, to
// open an entry and take its heaviest navigation: the threshold of A12's
// "panel ≤ 400 KB compressed", which this one constant holds.
//
// What the threshold is compared WITH is decision 2 of A12, and that
// decision is still open with the suite's owner. The proposal it measures
// today is budgeted() below: per entry, in gzip, the initial load plus the
// heaviest navigation (the deep link to Data Studio, for the panel). The
// total with every lazy screen loaded is measured and printed, and is not
// budgeted, because no operator downloads it in one go. If the owner picks
// another meaning, budgeted() is the one place that changes.
const compressedBudget = 400 * 1024

// budgeted is the measure compared with compressedBudget (decision 2 of
// A12, proposed and open): the entry's initial load plus its heaviest
// navigation, gzip-encoded.
func budgeted(l load) int64 { return l.initial.gzip + l.heaviest().size.gzip }

// The rule tools/precompress.ts writes the dist by: a text file of at least
// minVariantBytes carries <file>.gz and <file>.br, and a smaller one
// carries neither. Kept here rather than read from the TypeScript: when the
// two drift, the integrity test below fails in one direction or the other.
var compressible = regexp.MustCompile(`\.(?:js|mjs|css|html|svg|json|txt|xml|webmanifest)$`)

const minVariantBytes = 1024

func wantsVariants(name string, size int64) bool {
	return compressible.MatchString(name) && size >= minVariantBytes
}

func isVariant(name string) bool {
	return strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".br")
}

type entryDist struct {
	name string
	fsys fs.FS
}

func entries(t *testing.T) []entryDist {
	t.Helper()
	var out []entryDist
	for _, e := range []struct {
		name string
		get  func() fs.FS
	}{{"panel", Panel}, {"fleet", Fleet}} {
		dist := e.get()
		if dist == nil {
			t.Fatalf("embedded dist has no %s/index.html; run npm ci && npm run build in ui/", e.name)
		}
		if _, err := fs.Stat(dist, "assets"); err != nil {
			t.Fatalf("embedded %s has no assets/: %v", e.name, err)
		}
		out = append(out, entryDist{e.name, dist})
	}
	return out
}

// ---- what a browser fetches ------------------------------------------------------------------

// size is one file, or a set of them, as it travels: raw, and encoded the
// two ways the servers can send it.
type size struct{ raw, gzip, brotli int64 }

func (s *size) add(o size) { s.raw += o.raw; s.gzip += o.gzip; s.brotli += o.brotli }

// navigation is what leaving the entry's first screen for one lazily
// loaded screen fetches, beyond the initial load. A screen that itself
// loads more lazily (the dashboards' charts) is counted with everything it
// can load: the worst case of that navigation.
type navigation struct {
	name  string
	files []string
	size  size
}

type load struct {
	entry        string
	initialFiles []string
	initial      size
	navigations  []navigation // heaviest first
	total        size         // every file the entry can fetch
	reached      map[string]bool
}

func (l load) heaviest() navigation {
	if len(l.navigations) == 0 {
		return navigation{name: "(none)"}
	}
	return l.navigations[0]
}

var (
	// What the document loads: the entry script, its preloaded imports,
	// its stylesheet and its icon.
	documentRef = regexp.MustCompile(`(?:src|href)="\./([^"]+)"`)
	// A chunk's static imports, as the bundler writes them.
	staticImport = regexp.MustCompile(`(?:\bfrom|\bimport)\s*["']\./([^"'` + "`" + `]+)["']`)
	// A chunk's dynamic imports: a screen loaded on navigation.
	dynamicImport = regexp.MustCompile(`\bimport\(\s*["'` + "`" + `]\./([^"'` + "`" + `]+)["'` + "`" + `]\s*\)`)
	// The preload map Vite writes beside dynamic imports: the files each
	// one fetches with it, stylesheets included (a chunk does not import
	// its CSS; the preload helper links it).
	preloadFiles = regexp.MustCompile(`\bm\.f=\[([^\]]*)\]`)
	preloadCall  = regexp.MustCompile(`\bimport\(\s*` + "`" + `\./([^` + "`" + `]+)` + "`" + `\s*\)\s*,\s*__vite__mapDeps\(\[([0-9,]*)\]\)`)
	// What a stylesheet loads in turn: fonts and images by url().
	cssURL = regexp.MustCompile(`url\(\s*["']?\./([^"')]+)["']?\s*\)`)
)

// fileSize is one file's size raw and as each encoding travels: the
// sibling the build wrote, or the raw bytes for a file too small to carry
// one (it is served as it is).
func fileSize(t *testing.T, dist fs.FS, name string) size {
	t.Helper()
	info, err := fs.Stat(dist, name)
	if err != nil {
		t.Fatalf("the load names %s, which the dist does not carry: %v", name, err)
	}
	s := size{raw: info.Size(), gzip: info.Size(), brotli: info.Size()}
	if !wantsVariants(name, info.Size()) {
		return s
	}
	for _, v := range []struct {
		ext string
		to  *int64
	}{{".gz", &s.gzip}, {".br", &s.brotli}} {
		vi, err := fs.Stat(dist, name+v.ext)
		if err != nil {
			t.Fatalf("%s has no %s sibling: the dist was not precompressed (npm run build in ui/)", name, v.ext)
		}
		*v.to = vi.Size()
	}
	return s
}

// edges reads what fetching one file brings with it.
type edges struct {
	static  []string            // imported on load
	dynamic []string            // imported on navigation
	preload map[string][]string // dynamic target → files fetched with it
}

func readEdges(t *testing.T, dist fs.FS, name string) edges {
	t.Helper()
	content, err := fs.ReadFile(dist, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	dir := path.Dir(name)
	join := func(rel string) string { return path.Join(dir, rel) }
	var e edges
	switch path.Ext(name) {
	case ".css":
		for _, m := range cssURL.FindAllStringSubmatch(string(content), -1) {
			e.static = append(e.static, join(m[1]))
		}
		return e
	case ".js", ".mjs":
	default:
		return e
	}
	src := string(content)
	for _, m := range staticImport.FindAllStringSubmatch(src, -1) {
		e.static = append(e.static, join(m[1]))
	}
	for _, m := range dynamicImport.FindAllStringSubmatch(src, -1) {
		e.dynamic = append(e.dynamic, join(m[1]))
	}
	if strings.Contains(src, "__vite__mapDeps") {
		list := preloadFiles.FindStringSubmatch(src)
		calls := preloadCall.FindAllStringSubmatch(src, -1)
		if list == nil || len(calls) == 0 {
			t.Fatalf("%s uses Vite's preload map in a shape this measure does not read: the stylesheets its screens load would go uncounted", name)
		}
		var files []string
		for _, f := range strings.Split(list[1], ",") {
			f = strings.Trim(strings.TrimSpace(f), `"'`)
			if !strings.HasPrefix(f, "./") {
				t.Fatalf("%s: preload entry %q is not relative to the chunk", name, f)
			}
			files = append(files, join(strings.TrimPrefix(f, "./")))
		}
		e.preload = map[string][]string{}
		for _, c := range calls {
			target := join(c[1])
			for _, idx := range strings.Split(c[2], ",") {
				var i int
				if _, err := fmt.Sscanf(idx, "%d", &i); err != nil || i < 0 || i >= len(files) {
					t.Fatalf("%s: preload index %q out of range", name, idx)
				}
				e.preload[target] = append(e.preload[target], files[i])
			}
		}
	}
	return e
}

// measure walks an entry the way a browser does: the document's files and
// everything they import, then each screen a navigation can load.
func measure(t *testing.T, e entryDist) load {
	t.Helper()
	index, err := fs.ReadFile(e.fsys, "index.html")
	if err != nil {
		t.Fatalf("%s: read index.html: %v", e.name, err)
	}
	cache := map[string]edges{}
	edgesOf := func(name string) edges {
		if got, ok := cache[name]; ok {
			return got
		}
		got := readEdges(t, e.fsys, name)
		cache[name] = got
		return got
	}

	// closure: everything fetching roots brings, on load; with lazy, every
	// dynamic import below them as well (the worst case of a navigation).
	// A file in loaded is already in the browser: it is not walked again,
	// and the screens IT can load are navigations of their own.
	closure := func(roots []string, lazy bool, loaded map[string]bool) map[string]bool {
		seen := map[string]bool{}
		queue := append([]string(nil), roots...)
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if seen[name] || loaded[name] {
				continue
			}
			seen[name] = true
			ed := edgesOf(name)
			queue = append(queue, ed.static...)
			if lazy {
				for _, target := range ed.dynamic {
					queue = append(queue, target)
					queue = append(queue, ed.preload[target]...)
				}
			}
		}
		return seen
	}

	l := load{entry: e.name}
	var roots []string
	for _, m := range documentRef.FindAllStringSubmatch(string(index), -1) {
		roots = append(roots, m[1])
	}
	if len(roots) == 0 {
		t.Fatalf("%s: index.html references no ./ file: %s", e.name, index)
	}
	initial := closure(roots, false, nil)
	for name := range initial {
		l.initialFiles = append(l.initialFiles, name)
		l.initial.add(fileSize(t, e.fsys, name))
	}
	sort.Strings(l.initialFiles)

	// Each screen the first one can navigate to, with what it preloads.
	targets := map[string][]string{}
	for name := range initial {
		ed := edgesOf(name)
		for _, target := range ed.dynamic {
			targets[target] = append(targets[target], ed.preload[target]...)
		}
	}
	l.reached = map[string]bool{}
	for name := range initial {
		l.reached[name] = true
	}
	for target, preload := range targets {
		nav := navigation{name: strings.SplitN(path.Base(target), "-", 2)[0]}
		for name := range closure(append([]string{target}, preload...), true, initial) {
			l.reached[name] = true
			nav.files = append(nav.files, name)
			nav.size.add(fileSize(t, e.fsys, name))
		}
		sort.Strings(nav.files)
		l.navigations = append(l.navigations, nav)
	}
	sort.Slice(l.navigations, func(i, j int) bool {
		if l.navigations[i].size.gzip != l.navigations[j].size.gzip {
			return l.navigations[i].size.gzip > l.navigations[j].size.gzip
		}
		return l.navigations[i].name < l.navigations[j].name
	})
	for name := range l.reached {
		l.total.add(fileSize(t, e.fsys, name))
	}
	return l
}

func kib(n int64) string { return fmt.Sprintf("%.1f", float64(n)/1024) }

func (l load) report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s (KiB)                     raw     gzip   brotli\n", l.entry)
	row := func(label string, s size) {
		fmt.Fprintf(&b, "  %-26s %8s %8s %8s\n", label, kib(s.raw), kib(s.gzip), kib(s.brotli))
	}
	row("initial load", l.initial)
	for _, n := range l.navigations {
		row("+ "+n.name, n.size)
	}
	h := l.heaviest()
	sum := l.initial
	sum.add(h.size)
	row("initial + "+h.name, sum)
	row("total (every lazy screen)", l.total)
	fmt.Fprintf(&b, "  budgeted %s of %s KiB gzip\n", kib(budgeted(l)), kib(compressedBudget))
	return b.String()
}

// Each entry's initial load plus its heaviest navigation, gzip-encoded,
// stays within compressedBudget. The measure reads the built files and
// their encodings as a browser would fetch them, and every file of the
// entry must be reached by it — a file it cannot place means it missed an
// edge, and a budget over a graph with a missing edge measures less than
// what travels.
func TestEmbeddedDist_EachEntryWithinCompressedBudget(t *testing.T) {
	for _, e := range entries(t) {
		l := measure(t, e)
		t.Log(l.report())

		err := fs.WalkDir(e.fsys, "assets", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || isVariant(p) {
				return err
			}
			if !l.reached[p] {
				t.Errorf("%s: %s is in the dist and no load reaches it: the measure missed an import, or the build left a file nothing uses", e.name, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", e.name, err)
		}

		if got := budgeted(l); got > compressedBudget {
			h := l.heaviest()
			t.Errorf("%s: the initial load (%s KiB) plus its heaviest navigation, %s (%s KiB), is %s KiB gzip — over the budget of %s KiB.\n"+
				"The navigation fetches %v.\nA dependency joined a screen's chunk, or a screen is imported statically again (src/routes.ts).",
				e.name, kib(l.initial.gzip), h.name, kib(h.size.gzip), kib(got), kib(compressedBudget), h.files)
		}
	}
}

// The panel's Data Studio is its heaviest navigation, and its grid travels
// in a chunk of its own that nothing in the initial load imports.
func TestEmbeddedDist_PanelGridStaysOutOfTheInitialLoad(t *testing.T) {
	var panel entryDist
	for _, e := range entries(t) {
		if e.name == "panel" {
			panel = e
		}
	}
	l := measure(t, panel)
	if h := l.heaviest(); h.name != "DataStudioPage" {
		t.Errorf("the panel's heaviest navigation is %s, not Data Studio: %v", h.name, h.files)
	}
	for _, f := range l.initialFiles {
		if strings.HasPrefix(path.Base(f), "grid-") {
			t.Errorf("the initial load fetches the grid chunk %s", f)
		}
	}
}

// Every file that should travel compressed carries both encodings, the
// gzip one decodes to the file's bytes, and no encoding is left without
// its file. (The Brotli one is decoded where it is written, by the build;
// Go has no Brotli decoder in its standard library.)
func TestEmbeddedDist_PrecompressedSiblingsMatch(t *testing.T) {
	for _, e := range entries(t) {
		checked := 0
		err := fs.WalkDir(e.fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if isVariant(p) {
				if _, err := fs.Stat(e.fsys, p[:len(p)-3]); err != nil {
					t.Errorf("%s: %s has no file it encodes", e.name, p)
				}
				return nil
			}
			raw, err := fs.ReadFile(e.fsys, p)
			if err != nil {
				return err
			}
			gz, gzErr := fs.ReadFile(e.fsys, p+".gz")
			br, brErr := fs.ReadFile(e.fsys, p+".br")
			if !wantsVariants(p, int64(len(raw))) {
				if gzErr == nil || brErr == nil {
					t.Errorf("%s: %s (%d bytes) carries an encoding the rule does not give it", e.name, p, len(raw))
				}
				return nil
			}
			if gzErr != nil || brErr != nil {
				t.Errorf("%s: %s (%d bytes) lacks its .gz or .br: rebuild the dist (npm run build in ui/)", e.name, p, len(raw))
				return nil
			}
			zr, err := gzip.NewReader(bytes.NewReader(gz))
			if err != nil {
				t.Errorf("%s: %s.gz is not gzip: %v", e.name, p, err)
				return nil
			}
			decoded, err := io.ReadAll(zr)
			if err != nil || !bytes.Equal(decoded, raw) {
				t.Errorf("%s: %s.gz does not decode to %s (err %v): a stale encoding would serve old code", e.name, p, p, err)
			}
			if len(gz) >= len(raw) || len(br) >= len(raw) || len(br) == 0 {
				t.Errorf("%s: %s encodes to gzip %d / brotli %d bytes from %d", e.name, p, len(gz), len(br), len(raw))
			}
			checked++
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", e.name, err)
		}
		if checked == 0 {
			t.Errorf("%s: no file carries encodings at all", e.name)
		}
	}
}

// No font travels as a data: URL. The panel's policy says font-src 'self',
// and a stylesheet that inlines a font has it refused: the grid's icons
// were drawn in nothing for as long as the quartz stylesheet carried one
// (OR-63).
func TestEmbeddedDist_NoFontTravelsAsData(t *testing.T) {
	inline := regexp.MustCompile(`data:(?:font/|application/(?:x-)?font)`)
	for _, e := range entries(t) {
		err := fs.WalkDir(e.fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || isVariant(p) {
				return err
			}
			if ext := path.Ext(p); ext != ".css" && ext != ".js" && ext != ".html" {
				return nil
			}
			content, err := fs.ReadFile(e.fsys, p)
			if err != nil {
				return err
			}
			if loc := inline.FindIndex(content); loc != nil {
				t.Errorf("%s: %s carries a font as a data: URL (%q…), which font-src 'self' refuses", e.name, p, content[loc[0]:min(loc[1]+24, len(content))])
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", e.name, err)
		}
	}
}

// Dist is the two entries under their directory names.
func TestEmbeddedDist_DistJoinsBothEntries(t *testing.T) {
	var expected []string
	for _, e := range entries(t) {
		expected = append(expected, e.name+"/index.html")
		js, err := fs.Glob(e.fsys, "assets/*.js")
		if err != nil || len(js) == 0 {
			t.Fatalf("%s carries no script: %v", e.name, err)
		}
		expected = append(expected, e.name+"/"+js[0])
	}
	if err := fstest.TestFS(Dist(), expected...); err != nil {
		t.Fatal(err)
	}
}

// What a program carries is what it calls for. The linker keeps an
// embedded variable only when reachable code reads it, so a program that
// calls Fleet carries no byte of the panel's entry — the admin server's
// case — and one that calls Panel carries the panel's. Both programs are
// built here, against this module's own tree, and their binaries read.
func TestEmbeddedDist_EachEntryLinksAlone(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	if testing.Short() {
		t.Skip("builds two programs; skipped with -short")
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	goLine := regexp.MustCompile(`(?m)^go \S+$`).Find(mustRead(t, "go.mod"))

	// A marker of each entry that only its own embedding can put in a
	// binary: the name of the entry's hashed main script, which the file
	// table and the entry's index.html both carry.
	marker := func(dist fs.FS) []byte {
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`src="\./(assets/[^"]+\.js)"`).FindSubmatch(index)
		if m == nil {
			t.Fatalf("index.html names no entry script: %s", index)
		}
		return m[1]
	}
	panelMarker, fleetMarker := marker(Panel()), marker(Fleet())

	build := func(call string) []byte {
		dir := t.TempDir()
		files := map[string]string{
			"go.mod": "module example.com/linkcheck\n\n" + string(goLine) + "\n\n" +
				"require github.com/jcsvwinston/orbit/ui v0.0.0\n\n" +
				"replace github.com/jcsvwinston/orbit/ui => " + filepath.ToSlash(here) + "\n",
			"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"io/fs\"\n\n\tui \"github.com/jcsvwinston/orbit/ui\"\n)\n\n" +
				"func main() {\n\tb, _ := fs.ReadFile(ui." + call + "(), \"index.html\")\n\tfmt.Println(len(b))\n}\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out := filepath.Join(dir, "linkcheck")
		cmd := exec.Command(goBin, "build", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off", "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build a program that calls ui.%s: %v\n%s", call, err, output)
		}
		bin := mustRead(t, out)
		t.Logf("a program that calls ui.%s: %d bytes", call, len(bin))
		return bin
	}

	fleetOnly := build("Fleet")
	panelOnly := build("Panel")
	if !bytes.Contains(panelOnly, panelMarker) {
		t.Fatalf("the program that calls ui.Panel does not carry %s: the marker sees nothing", panelMarker)
	}
	if !bytes.Contains(fleetOnly, fleetMarker) {
		t.Fatalf("the program that calls ui.Fleet does not carry %s: the marker sees nothing", fleetMarker)
	}
	if bytes.Contains(fleetOnly, panelMarker) {
		t.Errorf("a program that calls only ui.Fleet carries the panel's entry (%s): the admin server would embed a panel it never serves", panelMarker)
	}
	if bytes.Contains(panelOnly, fleetMarker) {
		t.Errorf("a program that calls only ui.Panel carries the fleet's entry (%s)", fleetMarker)
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
