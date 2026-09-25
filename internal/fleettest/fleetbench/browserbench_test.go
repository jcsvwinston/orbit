// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"

	"github.com/jcsvwinston/orbit/agent"
	server "github.com/jcsvwinston/orbit/server"
)

// The browser half of the FLEET bench: what the admin server's UI does in a
// browser, measured by the same instrument the panel's bench uses
// (internal/adminbench/browser, project "fleet"). This driver lives here
// because only the test-only module may boot a real admin server and agent
// (ADR-006); the specs live next to the panel's so there is one Playwright
// project to install.

type browserCase struct {
	id    string
	title string
	want  verdict
	note  string
}

// browserCases is the recorded state of what the fleet UI does in a browser.
// The ids match the test names in internal/adminbench/browser/specs/fleet.spec.ts.
func browserCases() []browserCase {
	return []browserCase{
		{id: "UIF-00", title: "the instrument bites: a planted violation is caught", want: present},
		{id: "UIF-01", title: "the overview loads and lists the connected agent", want: present},
		{id: "UIF-02", title: "the fleet screens are legible: text meets contrast", want: absent,
			note: "the overview carries small muted text below 4.5:1 in the light theme (the default): the tagline and a " +
				"stat value on the --t5 cards among them. The sidebar's labels were raised to a passing token and the light " +
				"--t27/--t32 tokens raised as --t26 once was (A9 S10), and the rest is the re-skin onto the shared tokens " +
				"(OR-59), not another token nudged in isolation."},
		{id: "UIF-03", title: "every control says what it is: names, roles and labels", want: present},
		{id: "UIF-04", title: "the document says what it is: language, landmarks, one main heading", want: present},
		{id: "UIF-05", title: "the keyboard reaches the navigation, and the focus is visible", want: present},
	}
}

// TestFleetBrowserBench boots an admin server with a credential-less
// loopback operator and one agent, runs the fleet project of the browser
// instrument against the UI listener, and asserts the recorded verdicts. It
// SKIPS when the instrument is not installed; ORBIT_BENCH_BROWSER=required
// turns the skip into a failure, so the CI lane cannot go green without it.
func TestFleetBrowserBench(t *testing.T) {
	required := os.Getenv("ORBIT_BENCH_BROWSER") == "required"
	dir := filepath.Join("..", "..", "adminbench", "browser")
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@playwright", "test")); err != nil {
		msg := "the browser bench is not installed: cd internal/adminbench/browser && npm ci && npx playwright install --with-deps chromium"
		if required {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	e := newEnv(t)
	// A browser cannot set the trusted-proxy headers: the credential-less
	// loopback operator is how a person opens this UI in development.
	srv := e.startServer(t, server.Config{UIInsecureOpen: true, DataStudioAllowedModels: []string{"TestArticle"}})
	d, reg := e.agentDB(t, false)
	ag := e.startAgent(t, agent.Config{
		Endpoints:      []string{"http://" + srv.AgentAddr()},
		NodeIDOverride: "fb-browser",
		Registry:       reg,
		Databases:      map[string]*db.DB{"default": d},
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("the agent did not register")
	}
	results := filepath.Join(dir, "results-fleet.json")
	cmd := exec.Command("npx", "playwright", "test", "--project=fleet", "--reporter=json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"ORBIT_BENCH_URL=http://"+srv.UIAddr(),
		"ORBIT_BENCH_NODE_ID="+ag.NodeID(),
		"PLAYWRIGHT_JSON_OUTPUT_NAME=results-fleet.json",
	)
	output, runErr := cmd.CombinedOutput()
	got, err := parsePlaywrightResults(results)
	if err != nil {
		t.Fatalf("the browser bench produced no readable result (%v): %v\n%s", runErr, err, tail(string(output), 4000))
	}
	for _, c := range browserCases() {
		c := c
		t.Run(c.id, func(t *testing.T) {
			r, ok := got[c.id]
			if !ok {
				t.Fatalf("control %s has no test in browser/specs/fleet.spec.ts: the recorded verdict is measuring nothing", c.id)
			}
			v := absent
			switch r.status {
			case "passed":
				v = present
			case "skipped":
				t.Skipf("%s was skipped by the instrument", c.id)
			}
			if v != c.want {
				t.Errorf("control %s (%s) measures %q, the bench records %q.\n\n%s\n\n%s",
					c.id, c.title, v, c.want,
					"If the control just gained ground, that is the point: update the recorded\nverdict in browserbench_test.go in the same change.",
					tail(r.detail, 3000))
			}
		})
	}
}

type playwrightResult struct {
	status string
	detail string
}

// parsePlaywrightResults reads Playwright's JSON report and returns, per
// control id (the UIF-NN prefix of the test title), its status and the
// error text when it failed.
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
							Error  *struct {
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
						Error  *struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"results"`
				} `json:"tests"`
			} `json:"specs"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := map[string]playwrightResult{}
	record := func(title string, results []struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}) {
		id := strings.Fields(title)
		if len(id) == 0 || len(results) == 0 {
			return
		}
		last := results[len(results)-1]
		detail := ""
		if last.Error != nil {
			detail = last.Error.Message
		}
		out[id[0]] = playwrightResult{status: last.Status, detail: detail}
	}
	for _, s := range report.Suites {
		for _, spec := range s.Specs {
			for _, tst := range spec.Tests {
				record(spec.Title, tst.Results)
			}
		}
		for _, inner := range s.Suites {
			for _, spec := range inner.Specs {
				for _, tst := range spec.Tests {
					record(spec.Title, tst.Results)
				}
			}
		}
	}
	return out, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
