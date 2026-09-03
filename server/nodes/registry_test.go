package nodes

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestRegistry_AddListLookup(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry, deregister := r.Add(ctx, nil, NodeInfo{
		NodeID: "node-a", Version: "v1",
	}, 8)
	defer deregister()

	if entry.NodeID != "node-a" {
		t.Errorf("NodeID = %q", entry.NodeID)
	}
	if l := r.List(); len(l) != 1 || l[0].NodeID != "node-a" {
		t.Errorf("List = %+v", l)
	}
	if _, ok := r.Lookup("node-a"); !ok {
		t.Error("Lookup failed")
	}
	if _, ok := r.Lookup("ghost"); ok {
		t.Error("Lookup ghost should fail")
	}
}

func TestRegistry_RemoveOnDeregister(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, deregister := r.Add(ctx, nil, NodeInfo{NodeID: "node-a"}, 8)
	deregister()
	deregister() // idempotent

	if _, ok := r.Lookup("node-a"); ok {
		t.Error("Lookup after deregister should fail")
	}
}

func TestRegistry_TryEnqueue_NonBlocking(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry, deregister := r.Add(ctx, nil, NodeInfo{NodeID: "node-a"}, 1)
	defer deregister()

	frame := &adminv1.Frame{Body: &adminv1.Frame_Heartbeat{Heartbeat: &adminv1.Heartbeat{}}}
	if !TryEnqueue(entry, frame) {
		t.Error("first enqueue should succeed")
	}
	// Buffer is full; next enqueue must NOT block and must return false.
	if TryEnqueue(entry, frame) {
		t.Error("expected drop on full buffer")
	}
}

func TestRegistry_Touch(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, deregister := r.Add(ctx, nil, NodeInfo{NodeID: "node-a", LastSeenAt: time.Now().UTC().Add(-time.Hour)}, 8)
	defer deregister()

	r.Touch("node-a", time.Now().UTC())
	e, _ := r.Lookup("node-a")
	if time.Since(e.Info.LastSeenAt) > time.Minute {
		t.Errorf("LastSeenAt not updated: %v", e.Info.LastSeenAt)
	}
}

func TestRegistry_Watch(t *testing.T) {
	r := New()
	ch, cancel := r.Watch()
	defer cancel()

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	_, dereg := r.Add(ctx, nil, NodeInfo{NodeID: "node-a"}, 8)

	select {
	case change := <-ch:
		if !change.Connected || change.NodeID != "node-a" {
			t.Errorf("change = %+v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive connect notification")
	}

	dereg()

	select {
	case change := <-ch:
		if change.Connected || change.NodeID != "node-a" {
			t.Errorf("disconnect change = %+v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive disconnect notification")
	}
}

// TestRegistry_ReconnectCancelsSupersededStream pins the fix for the
// duplicate-stream bug: an agent that reconnects under the same NodeID
// before its old stream noticed the disconnect used to leave both
// streams alive (Add's eviction was a no-op closeOnce.Do), so every event
// reached the fleet twice. Add now cancels the superseded stream.
func TestRegistry_ReconnectCancelsSupersededStream(t *testing.T) {
	r := New()

	oldCtx, oldCancel := context.WithCancel(context.Background())
	oldEntry, oldDereg := r.Add(oldCtx, oldCancel, NodeInfo{NodeID: "node-a"}, 4)

	newCtx, newCancel := context.WithCancel(context.Background())
	defer newCancel()
	newEntry, newDereg := r.Add(newCtx, newCancel, NodeInfo{NodeID: "node-a"}, 4)
	defer newDereg()

	select {
	case <-oldEntry.CtxDone:
	case <-time.After(time.Second):
		t.Fatal("old stream context was not cancelled on reconnect")
	}
	if newCtx.Err() != nil {
		t.Fatal("the new stream must stay alive")
	}
	if e, ok := r.Lookup("node-a"); !ok || e != newEntry {
		t.Fatal("registry must point at the new entry")
	}

	// The old handler's deferred deregister runs after the cancel; it
	// must not evict the new entry.
	oldDereg()
	if e, ok := r.Lookup("node-a"); !ok || e != newEntry {
		t.Fatal("old handler's deregister evicted the new entry")
	}
	if TryEnqueue(oldEntry, &adminv1.Frame{}) {
		t.Fatal("enqueue on the superseded entry must fail once its stream is cancelled")
	}
}
