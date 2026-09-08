// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package routing

import (
	"strings"
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// FuzzNodeIDMatches is the property behind F12 (maturity audit 2026-09-03):
// the live bus and the replay buffer decide a NodeIds filter the same way. The
// two used to differ — exact on one side, folded on the other — so a filter
// matched the live events of a node and missed the replay of the same node,
// which reads to an operator as the node having gone quiet.
//
// Two properties, both over untrusted wire strings (a filter arrives from the
// UI, a node id from an agent):
//
//   - the two readers agree on every input, and both agree with the shared
//     matcher;
//   - matching is stable under padding, on either side, because both sides
//     trim before comparing.
//
// The seed corpus is the table of TestReplayMatches_NodeIDFoldsLikeTheBus plus
// the spellings an agent's identity file can hold (a UUID, a "region/host"
// path, an "@" form — see agent/identity).
func FuzzNodeIDMatches(f *testing.F) {
	seeds := []struct{ first, second, got string }{
		{" Node-A ", "other", "node-a"}, // F12 seed: folded and padded
		{"other", "", "node-a"},
		{"550e8400-e29b-41d4-a716-446655440000", "node-b", "550E8400-E29B-41D4-A716-446655440000"},
		{"region/us-east/host", "pod@host", "region/us-east/host"},
		{"", "", ""},
		{"node-a", "node-a", "node-b"},
		{"NODE-A\t", "\nnode-b", " node-b "},
	}
	for _, s := range seeds {
		f.Add(s.first, s.second, s.got)
	}

	f.Fuzz(func(t *testing.T, first, second, got string) {
		ids := []string{first, second}
		filter := &adminv1.Filter{NodeIds: ids}
		event := &adminv1.Event{NodeId: got}

		want := nodeIDMatches(ids, got)

		// The live path and the replay path are the same decision.
		sub := &EventSubscription{filter: filter}
		if live := sub.matches(event); live != want {
			t.Fatalf("live bus matched %v for ids %q, node %q; the shared matcher says %v", live, ids, got, want)
		}
		if replay := replayMatches(filter, event); replay != want {
			t.Fatalf("replay matched %v for ids %q, node %q; the live bus says %v", replay, ids, got, want)
		}

		// Padding is not identity: a filter typed with a stray space, or an
		// id that reached the wire padded, names the same node.
		padded := []string{" " + first + "\t", "\n" + second + " "}
		if p := nodeIDMatches(padded, " "+got+"\n"); p != want {
			t.Fatalf("padding changed the match for ids %q, node %q: %v vs %v", ids, got, p, want)
		}
		if p := replayMatches(&adminv1.Filter{NodeIds: padded}, &adminv1.Event{NodeId: "\t" + got + " "}); p != want {
			t.Fatalf("padding changed the replay match for ids %q, node %q: %v vs %v", ids, got, p, want)
		}

		// A match names one of the filter's ids, folded and trimmed: the
		// matcher never widens beyond its own list.
		if want {
			found := false
			for _, id := range ids {
				if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(got)) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("ids %q matched node %q, which is none of them", ids, got)
			}
		}

		// An empty NodeIds list is not a filter: both readers pass every
		// node, so a filter that names no node never silences one.
		empty := &adminv1.Filter{}
		if !replayMatches(empty, event) || !(&EventSubscription{filter: empty}).matches(event) {
			t.Fatalf("an empty NodeIds list rejected node %q", got)
		}
	})
}
