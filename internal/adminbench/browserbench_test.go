// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit"
)

// The browser half of the bench.
//
// Everything the Go probes measure, they measure through HTTP. Contrast,
// focus order, whether a dialog can be dismissed from the keyboard, whether a
// control has an accessible name: none of those exist until a browser has
// laid the page out and computed its styles. Asserting them from Go would be
// a claim, not a measurement — the audit of 2026-09-03 made exactly such a
// claim about this panel ("0 aria-*, contrast of 1.9:1"), and this is the
// instrument that decides whether it is still true by looking.
//
// The instrument is Playwright, driven from here so that it measures THE
// SAME application the rest of the bench does: this test boots it and hands
// over its URL. The verdicts live here, next to the HTTP ones, and the
// suite goes red when a control gains or loses ground.

// browserCase is one control of the browser bench, recorded the same way the
// HTTP ones are: what it asks, and what the panel answers today.
type browserCase struct {
	id    string
	title string
	want  verdict
	// note explains a verdict that is not present, in the payload's own
	// terms. An absence with no note is a bug in the bench.
	note string
	// failsWith is the failure an ABSENT control must fail with. A spec
	// fails when the capability is missing and also when it broke for any
	// other reason — a selector that no longer matches, a page that did not
	// load — and both read as absent. The specs that record an absence
	// check their preconditions first with messages of their own, and this
	// is the substring that says the failure is the one the control is
	// about.
	failsWith string
}

// browserCases is the recorded state of what the panel does in a browser.
// The ids match the test names in browser/specs/panel.spec.ts.
func browserCases() []browserCase {
	return []browserCase{
		{id: "UIX-00", title: "the instrument bites: a planted violation is caught", want: present},
		{id: "UIX-01", title: "the login screen is legible: text meets contrast", want: present},
		{id: "UIX-02", title: "the panel is legible on the screens an operator opens", want: present},
		{id: "UIX-03", title: "every control says what it is: names, roles and labels", want: present},
		{id: "UIX-04", title: "the document says what it is: language, landmarks, one main heading", want: present},
		{id: "UIX-05", title: "the keyboard reaches the navigation, and the focus is visible", want: present},
		{id: "UIX-06", title: "a dialog can be opened and dismissed from the keyboard", want: present},
		// The browser half of the extension family (A11), recorded at the
		// arc's baseline. UIX-07 closed in O1.
		{id: "UIX-07", title: "the login screen draws the logo the application declared", want: present},
		{id: "UIX-08", title: "the record view offers the action the application declared", want: absent,
			note:      "the grid offers an application action over a selection (DS-09); the record view's dialog has no button for it (EXT-02 is the contract half)",
			failsWith: "UIX-08: the record view offers no action"},
		// The browser half of EXT-01 (A11 O3): the form an action declared,
		// the server's refusal on the field it names, then the result.
		{id: "UIX-09", title: "an action that asks first draws its form and shows the refusal on the field", want: present},
		// The browser half of EXT-09, added in O2: the server's half
		// says what the document carries; this says what the browser
		// paints with it. (Numbered UIX-10 when the two stacks of the arc
		// met; O2's own pull request called it UIX-09.)
		{id: "UIX-10", title: "the first frame wears the theme the application configured, and the operator's own choice wins on reload", want: present},
		// The browser half of EXT-04 and EXT-05, added in O5.
		{id: "UIX-11", title: "a second dashboard draws a series to the operator granted it, and is neither listed nor served to one who is not", want: present},
	}
}

// The two operators UIX-11 signs in as: one granted the bench's second
// dashboard and one granted nothing. Both are created by the driver, with
// the bench's limited password, because the question is about what an
// operator WITHOUT the superuser's bypass is shown.
const (
	dashboardReader   = "dashboard-reader"
	dashboardOutsider = "dashboard-outsider"
)

// themedBranding is the second application UIX-10 opens: one that says its
// panel opens dark, with a dark surface of its own, so the spec can tell the
// configured theme from the panel's default dark and from the browser's
// preference (which the spec sets to light).
func themedBranding() orbit.Branding {
	return orbit.Branding{Theme: "dark", Dark: orbit.Palette{SurfaceColor: "#111827"}}
}

// TestBrowserBench runs the browser instrument against the bench's own
// application and asserts the recorded verdicts.
//
// It SKIPS when the instrument is not installed, and says how to install it.
// That is deliberate: a developer running `go test ./...` on a laptop should
// not be told their change broke something because a browser is missing. CI
// installs it, and ORBIT_BENCH_BROWSER=required turns the skip into a
// failure so the lane cannot go quietly green without it.
func TestBrowserBench(t *testing.T) {
	required := os.Getenv("ORBIT_BENCH_BROWSER") == "required"
	dir := filepath.Join("browser")
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@playwright", "test")); err != nil {
		msg := "the browser bench is not installed: cd internal/adminbench/browser && npm ci && npx playwright install --with-deps chromium"
		if required {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}

	e := newEnv(t)
	srv := e.server()
	// One request through the panel so the application is warm and the
	// bootstrap admin exists before the browser signs in.
	if r := e.get(t, "/admin/api/models"); r.code != 200 {
		t.Fatalf("the bench application is not serving: %d", r.code)
	}

	// UIX-11 needs an operator granted the second dashboard and one who is
	// not, and a series with something on it: three notes created today.
	reader := e.newOperator(t, dashboardReader, false)
	e.grant(t, reader.username, "admin:dashboard:"+trendsDashboard, "view")
	e.newOperator(t, dashboardOutsider, false)
	for i := 0; i < 3; i++ {
		e.createNote(t, map[string]any{"title": fmt.Sprintf("uix-11 note %d", i), "status": "draft"})
	}

	// UIX-10 needs an application that configured a theme, and the one the
	// rest of the bench measures must keep the panel's default: the
	// contrast controls read it as an operator opens it today.
	themed, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (themed)", Branding: themedBranding()}))
	if err != nil {
		t.Fatalf("the themed application refused to start: %v", err)
	}

	cmd := exec.Command("npx", "playwright", "test", "--project=panel", "--reporter=json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"ORBIT_BENCH_URL="+srv.URL(""),
		"ORBIT_BENCH_THEMED_URL="+themed.URL(""),
		"ORBIT_BENCH_THEMED_SURFACE="+themedBranding().Dark.SurfaceColor,
		"ORBIT_BENCH_USER=admin",
		"ORBIT_BENCH_PASSWORD="+bootstrapPassword,
		"ORBIT_BENCH_READER_USER="+dashboardReader,
		"ORBIT_BENCH_OUTSIDER_USER="+dashboardOutsider,
		"ORBIT_BENCH_OPERATOR_PASSWORD="+limitedPassword,
		"ORBIT_BENCH_DASHBOARD="+trendsDashboard,
		// Playwright writes its browsers here in CI; keep the default on a
		// developer machine.
		"PLAYWRIGHT_JSON_OUTPUT_NAME=results.json",
	)
	output, runErr := cmd.CombinedOutput()

	results, err := parsePlaywrightResults(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatalf("the browser bench produced no readable result (%v): %v\n%s", runErr, err, tail(string(output), 4000))
	}

	for _, c := range browserCases() {
		c := c
		t.Run(c.id, func(t *testing.T) {
			result, ok := results[c.id]
			if !ok {
				t.Fatalf("control %s has no test in browser/specs: the recorded verdict is measuring nothing", c.id)
			}
			got := absent
			switch result.status {
			case "passed":
				got = present
			case "skipped":
				t.Skipf("%s was skipped by the instrument", c.id)
			}
			if got != c.want {
				t.Errorf("control %s (%s) measures %q, the bench records %q.\n\n%s\n\n%s",
					c.id, c.title, got, c.want,
					"If the control just gained ground, that is the point: update the recorded\nverdict in browserbench_test.go in the same change.",
					tail(result.detail, 3000))
				return
			}
			if got == absent && c.failsWith != "" && !strings.Contains(result.detail, c.failsWith) {
				t.Errorf("control %s (%s) failed, but not with %q: the absence it records was not what was measured.\n\n%s",
					c.id, c.title, c.failsWith, tail(result.detail, 3000))
			}
		})
	}
}

// TestBrowserBenchSummary prints the browser half's numerator, the way the
// HTTP half prints its own. It is kept SEPARATE on purpose: a number that
// mixed what two different instruments can see would be a number nobody
// could check.
func TestBrowserBenchSummary(t *testing.T) {
	counts := map[verdict]int{}
	for _, c := range browserCases() {
		counts[c.want]++
	}
	t.Logf("\nbrowser  present %d · partial %d · absent %d  (of %d)\n",
		counts[present], counts[partial], counts[absent], len(browserCases()))
}

// playwrightResult is one spec's outcome.
type playwrightResult struct {
	status string
	detail string
}

// controlID picks the UIX-NN a spec title names, so the instrument's test
// names and the recorded verdicts cannot drift apart silently.
var controlID = regexp.MustCompile(`\b(UIX-\d+)\b`)

// parsePlaywrightResults reads the JSON report and keys it by control.
func parsePlaywrightResults(path string) (map[string]playwrightResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report struct {
		Suites []struct {
			Suites []struct {
				Specs []struct {
					Title string `json:"title"`
					Tests []struct {
						Results []struct {
							Status string `json:"status"`
							Error  struct {
								Message string `json:"message"`
							} `json:"error"`
						} `json:"results"`
					} `json:"tests"`
				} `json:"specs"`
			} `json:"suites"`
			Specs []struct {
				Title string `json:"title"`
				Tests []struct {
					Results []struct {
						Status string `json:"status"`
						Error  struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"results"`
				} `json:"tests"`
			} `json:"specs"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	out := map[string]playwrightResult{}
	record := func(title, status, detail string) {
		id := controlID.FindString(title)
		if id == "" {
			return
		}
		out[id] = playwrightResult{status: status, detail: detail}
	}
	for _, file := range report.Suites {
		for _, spec := range file.Specs {
			for _, test := range spec.Tests {
				for _, result := range test.Results {
					record(spec.Title, result.Status, result.Error.Message)
				}
			}
		}
		for _, group := range file.Suites {
			for _, spec := range group.Specs {
				for _, test := range spec.Tests {
					for _, result := range test.Results {
						record(spec.Title, result.Status, result.Error.Message)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no UIX control found in %s", path)
	}
	return out, nil
}

// tail keeps the end of a long output, which is where a failure's reason is.
func tail(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}
