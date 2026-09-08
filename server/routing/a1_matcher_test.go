// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package routing

import (
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// F12: the live bus and the replay buffer match NodeIds the same way —
// trimmed, case-insensitive — so a filter that matches live events also
// matches the replay of the same node.
func TestReplayMatches_NodeIDFoldsLikeTheBus(t *testing.T) {
	f := &adminv1.Filter{NodeIds: []string{" Node-A "}}
	e := &adminv1.Event{NodeId: "node-a"}
	if !replayMatches(f, e) {
		t.Fatal("replay: folded node id did not match")
	}
	if !nodeIDMatches(f.NodeIds, e.NodeId) {
		t.Fatal("shared matcher: folded node id did not match")
	}
	if replayMatches(&adminv1.Filter{NodeIds: []string{"other"}}, e) {
		t.Fatal("replay matched a different node")
	}
}

// The F12 rows the node-id fuzz target used to carry as seeds. They are a
// table, so they live in one: the spellings an agent's identity file holds (a
// UUID, a "region/host" path) and the padding either side can arrive with.
func TestNodeIDMatches_FoldsAndTrimsBothSides(t *testing.T) {
	for _, tc := range []struct {
		ids  []string
		node string
		want bool
	}{
		{[]string{" Node-A ", "other"}, "node-a", true},
		{[]string{"NODE-A\t"}, " node-a ", true},
		{[]string{"550e8400-e29b-41d4-a716-446655440000"}, "550E8400-E29B-41D4-A716-446655440000", true},
		{[]string{"region/us-east/host"}, "region/us-east/host", true},
		{[]string{"node-a"}, "node-b", false},
		{[]string{""}, "node-a", false},
		{[]string{"other", ""}, "", true},
	} {
		if got := nodeIDMatches(tc.ids, tc.node); got != tc.want {
			t.Errorf("nodeIDMatches(%q, %q) = %v, want %v", tc.ids, tc.node, got, tc.want)
		}
		// Whatever the shared matcher says, both readers say.
		f := &adminv1.Filter{NodeIds: tc.ids}
		e := &adminv1.Event{NodeId: tc.node}
		if live := (&EventSubscription{filter: f}).matches(e); live != tc.want {
			t.Errorf("the live bus decided %v for ids %q, node %q", live, tc.ids, tc.node)
		}
		if replay := replayMatches(f, e); replay != tc.want {
			t.Errorf("the replay buffer decided %v for ids %q, node %q", replay, tc.ids, tc.node)
		}
	}
}

// A3: a status class is a digit string (so says the proto field), and only a
// digit string names a status. "0:0" is three characters with a ':' in the
// tens place; the exact-status branch used to do arithmetic on it and match
// status 100, which no operator had asked for.
func TestStatusClassMatches_OnlyADigitStringNamesAStatus(t *testing.T) {
	for _, tc := range []struct {
		classes []string
		status  int
		want    bool
	}{
		{[]string{"5"}, 503, true},
		{[]string{"5"}, 404, false},
		{[]string{"503"}, 503, true},
		{[]string{"503"}, 500, true}, // a three-digit class still names its hundred
		{[]string{" 4 "}, 404, true},
		{[]string{"0:0"}, 100, false},
		{[]string{"1!0"}, 100, false},
		{[]string{"5a"}, 503, false},
		{[]string{"a"}, 200, false},
		{[]string{""}, 200, false},
		{nil, 200, true}, // an absent list is not a filter
	} {
		if got := statusClassMatches(tc.classes, tc.status); got != tc.want {
			t.Errorf("statusClassMatches(%q, %d) = %v, want %v", tc.classes, tc.status, got, tc.want)
		}
	}
}

// A3: a "/*" glob is a subtree, and the separators in front of it are
// normalised, so a doubled one is a typo and not a different filter. "//*"
// already meant "everything"; "/api//*" used to match "/api/" and nothing
// under it.
func TestPathMatches_SlashStarIsASubtree(t *testing.T) {
	for _, tc := range []struct {
		glob string
		path string
		want bool
	}{
		{"/api/*", "/api", true},
		{"/api/*", "/api/models", true},
		{"/api/*", "/apix", false},
		{"/api//*", "/api/", true},
		{"/api//*", "/api/models", true},
		{"//*", "/anything", true},
		{"/*", "", true},
		{"/api", "/api/models", true}, // a plain prefix is a prefix
		{"/healthz", "/healthz", true},
		{"/healthz", "/health", false},
	} {
		if got := pathMatches([]string{tc.glob}, tc.path); got != tc.want {
			t.Errorf("pathMatches(%q, %q) = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}
