// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

// Package adminbench is the measured inventory of what an operator can do
// with the panel today — one executable probe per control, and a RECORDED
// verdict the probe is checked against.
//
// It exists because the alternative is a capability table somebody writes by
// reading the code, and this repository already has one: the maturity audit
// of 2026-09-03 scored ten dimensions from a reading of v1.8.17. Four
// releases later, parts of that table are true, parts were fixed, and
// nothing says which is which. A table that nobody runs stops being true in
// silence.
//
// So no control here is judged by reading. Each one runs: it boots an
// application with orbit.Module mounted, signs in as the bootstrap admin,
// and asks the panel's own HTTP surface the question an operator would ask
// of the UI. A control that cannot be probed does not belong in the bench —
// what lives in the browser (contrast, focus order, keyboard reach) is
// measured where it lives, not asserted from Go.
//
// The test asserts the RECORDED verdict, not success. Closing a gap turns
// the suite red and asks for the verdict to be updated in the same change,
// so the numerator this bench publishes ("N of M controls present") moves
// with the code instead of behind it.
package adminbench

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// verdict is what a probe MEASURED, and what the case records.
type verdict string

const (
	// present: the control exists and this probe exercised it end to end.
	present verdict = "present"
	// partial: a piece of the control exists; the note says what is missing.
	partial verdict = "partial"
	// absent: no surface at all. The probe measures the absence — a 404 from
	// a mounted panel, an empty list, a field the schema never emits — never
	// the lack of a grep hit.
	absent verdict = "absent"
)

// control is one capability of the admin surface, with the probe that decides
// its verdict.
type control struct {
	id     string  // stable id, referenced by the arc plan and the registry
	family string  // grouping for the summary table
	title  string  // what the control is, in one line
	want   verdict // the RECORDED verdict — what the bench publishes
	note   string  // for partial/absent: what exactly is missing
	probe  func(t *testing.T, e *env) verdict
}

func TestAdminBench(t *testing.T) {
	cases := controls()
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.id] {
			t.Fatalf("duplicate control id %q", c.id)
		}
		seen[c.id] = true
		if c.probe == nil {
			t.Fatalf("%s has no probe: a control that cannot be measured does not belong in the bench", c.id)
		}
		if c.want != present && c.note == "" {
			t.Fatalf("%s is %s with no note: the gap has to say what is missing", c.id, c.want)
		}
	}

	e := newEnv(t)
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			got := c.probe(t, e)
			if got == c.want {
				return
			}
			t.Errorf("control %s (%s) measures %q, the bench records %q.\n\n"+
				"If the control just gained ground, that is the point: update the\n"+
				"recorded verdict in adminbench_cases_test.go in the same change, so\n"+
				"the published numerator moves with the code instead of behind it.",
				c.id, c.title, got, c.want)
		})
	}
}

// TestAdminBenchSummary prints the table the arc plan quotes. It asserts
// nothing: TestAdminBench is what fails when a verdict drifts.
func TestAdminBenchSummary(t *testing.T) {
	cases := controls()
	byFamily := map[string][]control{}
	for _, c := range cases {
		byFamily[c.family] = append(byFamily[c.family], c)
	}
	families := make([]string, 0, len(byFamily))
	for f := range byFamily {
		families = append(families, f)
	}
	sort.Strings(families)

	var b strings.Builder
	total := map[verdict]int{}
	for _, f := range families {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
			total[c.want]++
		}
		b.WriteString(fmt.Sprintf("%-24s present %d · partial %d · absent %d\n",
			f, count[present], count[partial], count[absent]))
	}
	b.WriteString(fmt.Sprintf("%-24s present %d · partial %d · absent %d  (of %d)\n",
		"TOTAL", total[present], total[partial], total[absent], len(cases)))
	t.Log("\n" + b.String())
}

// TestAdminBenchTable writes the markdown docs/admin-bench.md publishes, so
// the page is generated from the catalogue instead of being transcribed from
// it — the per-family summary and, family by family, every control with its
// verdict and what is missing. The fleet bench has had one since A9; this
// page was retyped by hand until the extension family (A11) made it a
// seventy-row transcription.
//
// It only writes when asked:
//
//	ORBIT_ADMIN_BENCH_TABLE=1 go test ./internal/adminbench/ -run TestAdminBenchTable
//
// Without the variable it is a no-op, so an ordinary `go test ./...`
// neither writes files nor fails on a read-only checkout. The file it
// writes, internal/adminbench/bench-table.md, is not committed.
func TestAdminBenchTable(t *testing.T) {
	if os.Getenv("ORBIT_ADMIN_BENCH_TABLE") == "" {
		t.Skip("set ORBIT_ADMIN_BENCH_TABLE=1 to regenerate the published table")
	}

	cases := controls()
	byFamily := map[string][]control{}
	order := []string{}
	for _, c := range cases {
		if _, seen := byFamily[c.family]; !seen {
			order = append(order, c.family)
		}
		byFamily[c.family] = append(byFamily[c.family], c)
	}

	var catalogue strings.Builder
	total := map[verdict]int{}
	for _, f := range order {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
			total[c.want]++
		}
		catalogue.WriteString(fmt.Sprintf("\n### %s — %d present · %d partial · %d absent\n\n",
			f, count[present], count[partial], count[absent]))
		catalogue.WriteString("| id | control | verdict | what is missing |\n|---|---|---|---|\n")
		for _, c := range byFamily[f] {
			note := c.note
			if note == "" {
				note = "—"
			}
			catalogue.WriteString(fmt.Sprintf("| `%s` | %s | **%s** | %s |\n",
				c.id, mdEscape(c.title), c.want, mdEscape(note)))
		}
	}
	header := fmt.Sprintf("**%d of %d controls present. %d partial. %d absent.**\n",
		total[present], len(cases), total[partial], total[absent])

	var summary strings.Builder
	summary.WriteString("\n| family | present | partial | absent |\n|---|---|---|---|\n")
	for _, f := range order {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
		}
		summary.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n", f, count[present], count[partial], count[absent]))
	}
	summary.WriteString(fmt.Sprintf("| **total** | **%d** | **%d** | **%d** |\n", total[present], total[partial], total[absent]))

	if err := os.WriteFile("bench-table.md", []byte(header+summary.String()+catalogue.String()), 0o644); err != nil {
		t.Fatalf("write the table: %v", err)
	}
	t.Logf("wrote bench-table.md: %s", strings.TrimSpace(header))
}

// mdEscape keeps a pipe inside a cell from ending it.
func mdEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}
