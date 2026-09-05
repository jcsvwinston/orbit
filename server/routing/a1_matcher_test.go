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
