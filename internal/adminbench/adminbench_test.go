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
