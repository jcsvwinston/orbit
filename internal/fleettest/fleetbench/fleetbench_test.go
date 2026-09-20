// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

// Package fleetbench is the measured inventory of what the fleet plane —
// agent, server, protocol and the fleet UI — can do today: one executable
// probe per control, and a RECORDED verdict the probe is checked against.
//
// It is the fleet counterpart of internal/adminbench, built for the same
// reason. The recon that opened arc A9 read the code and wrote down what it
// thought was there: mutual TLS but no identity binding, a Data Studio that
// does not speak the datasource contract, nothing that survives a restart,
// no alerts, one server, two single-page applications. A reading is a
// hypothesis. Each control here runs: it boots a real server.Server and a
// real agent.Agent with the configuration the question needs, drives the
// public surface — the Connect services on the UI listener, the agent
// listener, the metrics listener, the server's own State() — and reads the
// wire or the error it gets back. The static controls (the two SPAs, the
// protocol descriptors, the ADR index) read files and descriptors in the
// checked-out tree, never the absence of a word.
//
// The test asserts the RECORDED verdict, not success. Closing a gap turns
// the suite red and asks for the verdict to be updated in the same change,
// so the numerator this bench publishes ("N of M controls present") moves
// with the code instead of behind it.
package fleetbench

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
	// absent: no surface at all. The probe measures the absence — a stream
	// the server accepts under the wrong name, an empty replay after a
	// restart, a field the descriptor never declares — never the lack of a
	// grep hit.
	absent verdict = "absent"
)

// control is one capability of the fleet plane, with the probe that
// decides its verdict.
type control struct {
	id     string  // stable id, referenced by the arc plan and the registry
	family string  // grouping for the summary table
	title  string  // what the control is, in one line
	want   verdict // the RECORDED verdict — what the bench publishes
	note   string  // for partial/absent: what exactly is missing
	probe  func(t *testing.T, e *env) verdict
}

func TestFleetBench(t *testing.T) {
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
				"recorded verdict in cases_%s_test.go in the same change, so the\n"+
				"published numerator moves with the code instead of behind it.",
				c.id, c.title, got, c.want, c.family)
		})
	}
}

// TestFleetBenchSummary prints the table the arc plan quotes. It asserts
// nothing: TestFleetBench is what fails when a verdict drifts.
func TestFleetBenchSummary(t *testing.T) {
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
