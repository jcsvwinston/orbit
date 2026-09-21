// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
)

// The static probes read the repository as it is checked out: the two
// SPAs' manifests, the embed directives that decide which dist a binary
// ships, the workflow that gates them, the stylesheets that hold their
// tokens. They measure files and fields, never the absence of a word.

// ---- manifests --------------------------------------------------------------------------

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	SizeLimit       json.RawMessage   `json:"size-limit"`
}

func (e *env) packageJSON(t *testing.T, rel string) packageJSON {
	t.Helper()
	var pkg packageJSON
	if err := json.Unmarshal([]byte(e.readFile(t, rel)), &pkg); err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return pkg
}

// semverMajor reads the major of a dependency range such as "^1.6.1" or
// "~5.9.3"; -1 when there is none to read.
func semverMajor(spec string) int {
	spec = strings.TrimLeft(strings.TrimSpace(spec), "^~>=<v ")
	if i := strings.IndexAny(spec, ".- "); i >= 0 {
		spec = spec[:i]
	}
	n, err := strconv.Atoi(spec)
	if err != nil {
		return -1
	}
	return n
}

// ---- embed directives -------------------------------------------------------------------

// embedTargets resolves the //go:embed patterns of a Go file to absolute
// directories: what a binary built from that file ships.
func (e *env) embedTargets(t *testing.T, rel string) []string {
	t.Helper()
	dir := filepath.Dir(filepath.Join(e.repoRoot(), rel))
	var out []string
	for _, line := range strings.Split(e.readFile(t, rel), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:embed") {
			continue
		}
		for _, pattern := range strings.Fields(strings.TrimPrefix(line, "//go:embed")) {
			pattern = strings.TrimPrefix(pattern, "all:")
			pattern = strings.TrimSuffix(pattern, "/*")
			out = append(out, filepath.Clean(filepath.Join(dir, pattern)))
		}
	}
	return out
}

// ---- imports ----------------------------------------------------------------------------

var importRefs = []*regexp.Regexp{
	regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`),
	regexp.MustCompile(`\bfrom\s+['"]([^'"]+)['"]`),
	regexp.MustCompile(`@import\s+(?:url\()?\s*['"]([^'"]+)['"]`),
}

// importedPaths lists the files a stylesheet or config imports, resolved
// to absolute paths. A relative specifier resolves against the importing
// file; a bare package specifier counts only when it resolves INSIDE this
// repository — a workspace or file: dependency whose node_modules entry
// links back into the tree — because a shared token source has to be
// shared code, not a package both happen to install.
func (e *env) importedPaths(rel string) map[string]bool {
	abs := filepath.Join(e.repoRoot(), rel)
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, re := range importRefs {
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			spec := m[1]
			if strings.HasPrefix(spec, ".") {
				out[filepath.Clean(filepath.Join(filepath.Dir(abs), spec))] = true
				continue
			}
			if p := e.resolveBareSpecifier(filepath.Dir(abs), spec); p != "" {
				out[p] = true
			}
		}
	}
	return out
}

// resolveBareSpecifier walks up from dir looking for node_modules/<pkg>,
// follows symlinks and returns the target when it lies inside the
// repository; "" otherwise.
func (e *env) resolveBareSpecifier(dir, spec string) string {
	parts := strings.Split(spec, "/")
	pkg := parts[0]
	rest := parts[1:]
	if strings.HasPrefix(pkg, "@") && len(parts) > 1 {
		pkg = parts[0] + "/" + parts[1]
		rest = parts[2:]
	}
	root := e.repoRoot()
	for d := dir; strings.HasPrefix(d, root); d = filepath.Dir(d) {
		candidate := filepath.Join(d, "node_modules", pkg)
		target, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			if d == root {
				break
			}
			continue
		}
		if strings.HasPrefix(target, root+string(filepath.Separator)) {
			return filepath.Clean(filepath.Join(append([]string{target}, rest...)...))
		}
		return ""
	}
	return ""
}

// ---- workflow -----------------------------------------------------------------------------

var workflowJobHeader = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

// workflowJobs splits a GitHub workflow into its jobs (two-space keys
// under jobs:) and returns each job's lines.
func workflowJobs(yaml string) map[string][]string {
	jobs := map[string][]string{}
	inJobs := false
	current := ""
	for _, line := range strings.Split(yaml, "\n") {
		if strings.TrimSpace(line) == "jobs:" && !strings.HasPrefix(line, " ") {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if m := workflowJobHeader.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current != "" {
			jobs[current] = append(jobs[current], line)
		}
	}
	return jobs
}

// jobWorksIn reports whether a job runs in dir: as its default
// working-directory or in any of its steps. "./ui", "ui/" and "ui" are
// the same directory.
func jobWorksIn(lines []string, dir string) bool {
	want := normaliseDir(dir)
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "working-directory:") && normaliseDir(strings.TrimPrefix(l, "working-directory:")) == want {
			return true
		}
	}
	return false
}

func normaliseDir(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.TrimPrefix(s, "./")
	return strings.TrimSuffix(s, "/")
}

// jobDiffsDist reports whether a job compares the committed dist with
// what the build produced: a git diff or status over a dist path.
func jobDiffsDist(lines []string) bool {
	text := strings.Join(lines, "\n")
	return strings.Contains(text, "dist") &&
		(strings.Contains(text, "git diff") || strings.Contains(text, "git status --porcelain"))
}

// jobNames reports whether a job's text names a path.
func jobNames(lines []string, path string) bool {
	return strings.Contains(strings.Join(lines, "\n"), path)
}

// ---- specs and budgets ------------------------------------------------------------------------

var (
	// A navigation, not any path-looking string: page.route('/api/*') or a
	// unit test's '/login' would otherwise count as a visit.
	specGoto      = regexp.MustCompile(`\.goto\(\s*['"` + "`" + `](/[^'"` + "`" + `\s]*)['"` + "`" + `]`)
	budgetJS      = regexp.MustCompile(`initialJSBudget\s*=\s*(\d+)\s*\*\s*1024`)
	budgetCSS     = regexp.MustCompile(`initialCSSBudget\s*=\s*(\d+)\s*\*\s*1024`)
	budgetJSUsed  = regexp.MustCompile(`>\s*initialJSBudget`)
	budgetCSSUsed = regexp.MustCompile(`>\s*initialCSSBudget`)
	// Any byte budget a test names: `fooBudget = 512 * 1024`, `budgetBytes = 400_000`.
	anyBudget     = regexp.MustCompile(`(?i)budget\w*\s*=\s*\d[\d_]*(\s*\*\s*1024)?`)
	indexAssetRef = regexp.MustCompile(`(?:src|href)="\.?/assets/([^"]+)"`)
	bufGenPlugin  = regexp.MustCompile(`buf\.build/(bufbuild|connectrpc)/es(?::v(\d+))?`)
	yamlComment   = regexp.MustCompile(`(?m)#.*$`)
)

// walkFiles lists the files under root (relative to the repository) whose
// name matches keep, skipping node_modules and dist trees.
func (e *env) walkFiles(t *testing.T, root string, keep func(name string) bool) []string {
	t.Helper()
	base := filepath.Join(e.repoRoot(), root)
	var out []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a missing root is an empty result, not a failure
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if keep(d.Name()) {
			rel, _ := filepath.Rel(e.repoRoot(), p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func isSpecFile(name string) bool {
	return strings.Contains(name, ".spec.") || strings.Contains(name, ".test.")
}

// ---- probes -----------------------------------------------------------------------------------------

// UI-01: one frontend project serves both planes — the fleet server and
// the in-process panel embed the same dist.
func probeOneFrontendProject(t *testing.T, e *env) verdict {
	fleet := e.embedTargets(t, "server/ui/embed.go")
	var panel []string
	for _, p := range e.embedTargets(t, "internal/admin/ui_fallback.go") {
		if strings.HasSuffix(p, "dist") {
			panel = append(panel, p)
		}
	}
	if len(fleet) == 0 || len(panel) == 0 {
		t.Fatalf("embed directives not found: fleet=%v panel=%v", fleet, panel)
	}
	projects := 0
	for _, m := range []string{"ui/package.json", "internal/admin/ui/package.json"} {
		if fileExists(filepath.Join(e.repoRoot(), m)) {
			projects++
		}
	}
	t.Logf("fleet embeds %s; panel embeds %s; %d package.json projects", fleet[0], panel[0], projects)
	switch {
	case fleet[0] == panel[0]:
		return present
	case projects <= 1:
		return partial
	}
	return absent
}

// UI-02: the two planes share design tokens — one token source imported
// by both, or one project.
func probeSharedDesignTokens(t *testing.T, e *env) verdict {
	const fleetCSS, panelCSS = "ui/src/index.css", "internal/admin/ui/src/index.css"
	if !fileExists(filepath.Join(e.repoRoot(), "ui/package.json")) || !fileExists(filepath.Join(e.repoRoot(), "internal/admin/ui/package.json")) {
		t.Log("a single frontend project: its tokens are shared by construction")
		return present
	}
	fleet := map[string]bool{}
	for _, f := range []string{"ui/tailwind.config.js", "ui/tailwind.config.ts", fleetCSS} {
		for p := range e.importedPaths(f) {
			fleet[p] = true
		}
	}
	var shared []string
	for _, f := range []string{"internal/admin/ui/tailwind.config.js", "internal/admin/ui/tailwind.config.ts", panelCSS} {
		for p := range e.importedPaths(f) {
			if fleet[p] {
				shared = append(shared, p)
			}
		}
	}
	if len(shared) > 0 {
		t.Logf("both planes import %v", shared)
		return present
	}
	fleetTokens := strings.Contains(e.readFile(t, fleetCSS), ":root")
	panelTokens := strings.Contains(e.readFile(t, panelCSS), ":root")
	if fleetTokens && panelTokens {
		t.Log("each stylesheet declares its own :root custom properties and neither imports a shared file")
		return absent
	}
	t.Logf("token declarations: fleet=%v panel=%v, nothing shared", fleetTokens, panelTokens)
	return partial
}

// UI-03: the fleet UI has automated tests: a runner, a script that runs
// it, and at least one spec.
func probeFleetUITests(t *testing.T, e *env) verdict {
	pkg := e.packageJSON(t, "ui/package.json")
	_, hasScript := pkg.Scripts["test"]
	runner := false
	for _, r := range []string{"vitest", "jest", "@playwright/test", "mocha", "@web/test-runner"} {
		if _, ok := pkg.DevDependencies[r]; ok {
			runner = true
		}
	}
	specs := e.walkFiles(t, "ui", isSpecFile)
	t.Logf("test script=%v runner=%v specs=%d", hasScript, runner, len(specs))
	switch {
	case hasScript && runner && len(specs) > 0:
		return present
	case hasScript || runner || len(specs) > 0:
		return partial
	}
	return absent
}

// UI-04: CI checks that the fleet UI's committed dist is fresh, the way
// the panel's lane does. The panel's lane is the positive control for the
// parser: if it cannot see that one, it measures nothing.
func probeFleetDistFreshnessGate(t *testing.T, e *env) verdict {
	jobs := workflowJobs(e.readFile(t, ".github/workflows/ci.yml"))
	adminUI, ok := jobs["admin-ui"]
	if !ok || !jobDiffsDist(adminUI) {
		t.Fatalf("the parser does not see the admin-ui freshness step (job found: %v)", ok)
	}
	lanes := 0
	for name, lines := range jobs {
		if name == "admin-ui" {
			continue
		}
		inFleet := jobWorksIn(lines, "ui") || jobNames(lines, "server/ui/dist")
		if !inFleet {
			continue
		}
		lanes++
		if jobDiffsDist(lines) {
			t.Logf("job %q diffs the fleet dist", name)
			return present
		}
	}
	t.Logf("%d job(s) touch the fleet UI and none diffs server/ui/dist after building", lanes)
	return absent
}

// UI-05: a bundle-size budget covers the fleet UI — a Go test with a
// budget constant over server/ui/dist, a size-limit configuration in the
// fleet project, or a CI step that enforces one.
func probeFleetBundleBudget(t *testing.T, e *env) verdict {
	for _, f := range e.walkFiles(t, "server/ui", func(n string) bool { return strings.HasSuffix(n, "_test.go") }) {
		if m := anyBudget.FindString(e.readFile(t, f)); m != "" {
			t.Logf("%s names a budget: %s", f, m)
			return present
		}
	}
	pkg := e.packageJSON(t, "ui/package.json")
	configured := len(pkg.SizeLimit) > 0
	if _, ok := pkg.Scripts["size"]; ok {
		configured = true
	}
	tool := false
	for _, dep := range []string{"size-limit", "@size-limit/preset-app", "bundlesize", "bundlewatch"} {
		if _, ok := pkg.DevDependencies[dep]; ok {
			tool = true
		}
	}
	jobs := workflowJobs(e.readFile(t, ".github/workflows/ci.yml"))
	ciEnforces := false
	for _, lines := range jobs {
		text := strings.Join(lines, "\n")
		if jobWorksIn(lines, "ui") && (strings.Contains(text, "size-limit") || strings.Contains(text, "bundlesize") || strings.Contains(text, "bundlewatch")) {
			ciEnforces = true
		}
	}
	switch {
	case (configured && tool) || ciEnforces:
		return present
	case configured || tool:
		t.Logf("size tooling half-wired: configured=%v tool=%v", configured, tool)
		return partial
	}
	return absent
}

func budgetConstants(src string) (js, css int64, ok bool) {
	mj := budgetJS.FindStringSubmatch(src)
	mc := budgetCSS.FindStringSubmatch(src)
	if mj == nil || mc == nil {
		return 0, 0, false
	}
	j, _ := strconv.ParseInt(mj[1], 10, 64)
	c, _ := strconv.ParseInt(mc[1], 10, 64)
	return j * 1024, c * 1024, true
}

// UI-06: the fleet UI's generated stubs are connect-es 2 / protobuf-es 2:
// the runtime dependencies AND the generators that emit the stubs. In the
// second generation the services come out of bufbuild/es itself, so the
// connectrpc/es generator is either gone or at least v2.
func probeConnectES2(t *testing.T, e *env) verdict {
	pkg := e.packageJSON(t, "ui/package.json")
	connectMajor := semverMajor(pkg.Dependencies["@connectrpc/connect"])
	webMajor := semverMajor(pkg.Dependencies["@connectrpc/connect-web"])
	pbMajor := semverMajor(pkg.Dependencies["@bufbuild/protobuf"])
	depsV2 := connectMajor >= 2 && webMajor >= 2 && pbMajor >= 2

	generators := map[string]int{}
	bufGen := yamlComment.ReplaceAllString(e.readFile(t, "proto/buf.gen.yaml"), "")
	for _, m := range bufGenPlugin.FindAllStringSubmatch(bufGen, -1) {
		v := -1
		if m[2] != "" {
			v, _ = strconv.Atoi(m[2])
		}
		generators[m[1]] = v
	}
	connectGen, hasConnectGen := generators["connectrpc"]
	genV2 := generators["bufbuild"] >= 2 && (!hasConnectGen || connectGen >= 2)
	t.Logf("@connectrpc/connect %d, @connectrpc/connect-web %d, @bufbuild/protobuf %d; generators %v", connectMajor, webMajor, pbMajor, generators)
	switch {
	case depsV2 && genV2:
		return present
	case depsV2 || genV2:
		return partial
	}
	return absent
}

// underPlaywrightProject reports whether a repository file sits inside a
// directory tree that holds a playwright.config.*: a spec outside one is a
// unit test, not a browser visit.
func (e *env) underPlaywrightProject(rel string) bool {
	root := e.repoRoot()
	for d := filepath.Dir(filepath.Join(root, rel)); strings.HasPrefix(d, root); d = filepath.Dir(d) {
		if matches, _ := filepath.Glob(filepath.Join(d, "playwright.config.*")); len(matches) > 0 {
			return true
		}
		if d == root {
			break
		}
	}
	return false
}

// UI-07: the browser instrument covers the fleet UI: a Playwright spec
// navigates to a path outside the panel's /admin prefix.
func probeBrowserInstrumentCoversFleet(t *testing.T, e *env) verdict {
	var specs []string
	for _, root := range []string{"internal", "ui"} {
		for _, f := range e.walkFiles(t, root, func(n string) bool {
			return strings.HasSuffix(n, ".spec.ts") || strings.HasSuffix(n, ".spec.js") || strings.HasSuffix(n, ".spec.mts")
		}) {
			if e.underPlaywrightProject(f) {
				specs = append(specs, f)
			}
		}
	}
	if len(specs) == 0 {
		t.Log("no Playwright spec anywhere")
		return absent
	}
	var fleetVisits []string
	for _, f := range specs {
		for _, m := range specGoto.FindAllStringSubmatch(e.readFile(t, f), -1) {
			if !strings.HasPrefix(m[1], "/admin") {
				fleetVisits = append(fleetVisits, f+" → "+m[1])
			}
		}
	}
	if len(fleetVisits) > 0 {
		t.Logf("fleet paths visited: %v", fleetVisits)
		return present
	}
	t.Logf("%d Playwright spec file(s), every goto() under /admin", len(specs))
	return absent
}

// UI-08: the fleet UI is told the operator's role: GetSelf says read-only
// for a viewer and not for a plain operator.
func probeUIKnowsRole(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	ctx := ctxFor(t)
	viewer, err := e.controlAs(srv.Server, viewerHeaders()).GetSelf(ctx, connect.NewRequest(&adminv1.GetSelfRequest{}))
	if err != nil {
		t.Fatalf("GetSelf as viewer: %v", err)
	}
	plain, err := e.control(srv.Server).GetSelf(ctx, connect.NewRequest(&adminv1.GetSelfRequest{}))
	if err != nil {
		t.Fatalf("GetSelf: %v", err)
	}
	switch {
	case viewer.Msg.GetReadOnly() && !plain.Msg.GetReadOnly() && plain.Msg.GetSubject() == operatorName:
		return present
	case !viewer.Msg.GetReadOnly():
		t.Logf("viewer answered read_only=false role=%q", viewer.Msg.GetRole())
		return absent
	}
	t.Logf("plain operator answered read_only=%v subject=%q", plain.Msg.GetReadOnly(), plain.Msg.GetSubject())
	return partial
}

// UI-09: the fleet UI has a tenant notion: some message on the wire
// carries one.
func probeFleetTenantNotion(t *testing.T, e *env) verdict {
	if fields := anyFieldContaining("tenant"); len(fields) > 0 {
		t.Logf("tenant fields on the wire: %v", fields)
		return present
	}
	return absent
}

// UI-10: the panel's initial load stays within its budget, and the budget
// is a test constant the test COMPARES against — a constant nothing reads
// is a number in a document with a Go extension.
func probePanelBudgetEnforced(t *testing.T, e *env) verdict {
	src := e.readFile(t, "internal/admin/ui_embed_test.go")
	js, css, ok := budgetConstants(src)
	if !ok {
		t.Log("internal/admin/ui_embed_test.go names no initial JS/CSS budget")
		return absent
	}
	if !budgetJSUsed.MatchString(src) || !budgetCSSUsed.MatchString(src) {
		t.Logf("the budget constants exist but the test does not compare against both: js compared=%v css compared=%v", budgetJSUsed.MatchString(src), budgetCSSUsed.MatchString(src))
		return partial
	}
	index := e.readFile(t, "internal/admin/ui/dist/index.html")
	refs := indexAssetRef.FindAllStringSubmatch(index, -1)
	if len(refs) == 0 {
		t.Fatal("the panel's index.html references no ./assets/*")
	}
	var sumJS, sumCSS int64
	for _, m := range refs {
		size := e.fileSize("internal/admin/ui/dist/assets/" + m[1])
		if size < 0 {
			t.Fatalf("index.html references assets/%s, which is not in the dist", m[1])
		}
		switch path.Ext(m[1]) {
		case ".js":
			sumJS += size
		case ".css":
			sumCSS += size
		}
	}
	t.Logf("initial load: js=%d/%d css=%d/%d", sumJS, js, sumCSS, css)
	if sumJS <= js && sumCSS <= css {
		return present
	}
	return partial
}
