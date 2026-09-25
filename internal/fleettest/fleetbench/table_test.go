// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestFleetBenchTable writes the markdown table that docs/fleet-bench.md
// publishes, so the page is generated from the catalogue instead of being
// transcribed from it. A table somebody retypes is a table that drifts, and
// the number it carries is the one the arc gate quotes.
//
// It only writes when asked:
//
//	ORBIT_FLEET_BENCH_TABLE=1 go test ./fleetbench/ -run TestFleetBenchTable
//
// (from internal/fleettest). Without the variable it is a no-op, so an
// ordinary `go test ./...` neither writes files nor fails on a read-only
// checkout.
func TestFleetBenchTable(t *testing.T) {
	if os.Getenv("ORBIT_FLEET_BENCH_TABLE") == "" {
		t.Skip("set ORBIT_FLEET_BENCH_TABLE=1 to regenerate the published table")
	}

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
		rows := byFamily[f]
		sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
		count := map[verdict]int{}
		for _, c := range rows {
			count[c.want]++
			total[c.want]++
		}
		b.WriteString(fmt.Sprintf("\n### %s — %d present · %d partial · %d absent\n\n",
			f, count[present], count[partial], count[absent]))
		b.WriteString("| id | control | verdict | what is missing |\n|---|---|---|---|\n")
		for _, c := range rows {
			note := c.note
			if note == "" {
				note = "—"
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | **%s** | %s |\n",
				c.id, mdEscape(c.title), c.want, mdEscape(note)))
		}
	}
	header := fmt.Sprintf("**%d of %d controls present. %d partial. %d absent.**\n",
		total[present], len(cases), total[partial], total[absent])

	// The per-family summary the page opens with. It used to be retyped
	// by hand and was three sessions stale while the headline above it was
	// current; the umbrella's fleet-posture guard now compares both with
	// the catalogue, so it is generated here with the rest.
	var summary strings.Builder
	summary.WriteString("\n| family | present | partial | absent |\n|---|---|---|---|\n")
	for _, f := range families {
		count := map[verdict]int{}
		for _, c := range byFamily[f] {
			count[c.want]++
		}
		summary.WriteString(fmt.Sprintf("| %s | %d | %d | %d |\n", f, count[present], count[partial], count[absent]))
	}
	summary.WriteString(fmt.Sprintf("| **total** | **%d** | **%d** | **%d** |\n", total[present], total[partial], total[absent]))

	if err := os.WriteFile("bench-table.md", []byte(header+summary.String()+b.String()), 0o644); err != nil {
		t.Fatalf("write the table: %v", err)
	}
	t.Logf("wrote bench-table.md: %s", strings.TrimSpace(header))
}

// mdEscape keeps a pipe inside a cell from ending it.
func mdEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}
