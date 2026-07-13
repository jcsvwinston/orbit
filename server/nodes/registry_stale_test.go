package nodes

// Tests for the inactivity leg (OR-SEC-P2-4): MarkStale flips a silent
// node offline without evicting it, and Touch revives it when frames
// resume.

import (
	"context"
	"testing"
	"time"
)

func TestRegistry_MarkStaleAndTouchRevive(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes, stop := r.Watch()
	defer stop()

	_, deregister := r.Add(ctx, NodeInfo{NodeID: "node-a"}, 4)
	defer deregister()
	<-changes // connect notification

	if !r.MarkStale("node-a") {
		t.Fatal("MarkStale returned false for a connected node")
	}
	select {
	case ch := <-changes:
		if ch.Connected {
			t.Errorf("stale notification Connected = true")
		}
	case <-time.After(time.Second):
		t.Fatal("no watcher notification on MarkStale")
	}
	if infos := r.List(); len(infos) != 1 || infos[0].Connected {
		t.Fatalf("List after MarkStale = %+v, want one disconnected entry", infos)
	}

	// Idempotent: a second MarkStale is a no-op.
	if r.MarkStale("node-a") {
		t.Error("second MarkStale returned true")
	}

	// A frame arrives → Touch revives the node and notifies watchers.
	r.Touch("node-a", time.Now().UTC())
	select {
	case ch := <-changes:
		if !ch.Connected {
			t.Errorf("revive notification Connected = false")
		}
	case <-time.After(time.Second):
		t.Fatal("no watcher notification on Touch revive")
	}
	if infos := r.List(); len(infos) != 1 || !infos[0].Connected {
		t.Fatalf("List after revive = %+v, want one connected entry", infos)
	}
}
