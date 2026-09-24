package nodes

import (
	"context"
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestRegistry_RemoteEntries(t *testing.T) {
	r := New()
	r.SetRemote(NodeInfo{NodeID: "n1", Connected: true, RegisteredModels: []string{"Article"}}, "server-b")
	e, ok := r.Lookup("n1")
	if !ok || !e.Remote() || e.Info.Via != "server-b" {
		t.Fatalf("a remote announcement is listed as remote: %+v ok=%v", e, ok)
	}
	if TryEnqueue(e, &adminv1.Frame{}) {
		t.Fatal("nothing may be enqueued to a remote entry")
	}
	if _, ok := r.AnyWithModel("Article"); ok {
		t.Fatal("a remote node cannot serve a Data Studio request here")
	}
	if got := r.Local(); len(got) != 0 {
		t.Fatalf("Local lists only this server's nodes, got %v", got)
	}
	if got := r.List(); len(got) != 1 {
		t.Fatalf("List includes remote nodes, got %d", len(got))
	}

	// A local registration wins over the remote entry and is not evicted by a later announcement.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	local, done := r.Add(ctx, cancel, NodeInfo{NodeID: "n1"}, 1)
	defer done()
	r.SetRemote(NodeInfo{NodeID: "n1", Connected: true}, "server-b")
	e, _ = r.Lookup("n1")
	if e != local || e.Remote() {
		t.Fatal("a node connected here is never replaced by a remote announcement")
	}
	if ctx.Err() != nil {
		t.Fatal("registering over a remote entry must not cancel anything")
	}

	// Remote removal, one node and a whole peer.
	r.SetRemote(NodeInfo{NodeID: "n2"}, "server-b")
	r.SetRemote(NodeInfo{NodeID: "n3"}, "server-c")
	r.RemoveRemote("n2", "server-b")
	if _, ok := r.Lookup("n2"); ok {
		t.Fatal("n2 was announced gone")
	}
	r.RemoveRemote("", "server-c")
	if _, ok := r.Lookup("n3"); ok {
		t.Fatal("server-c left: its nodes go with it")
	}
	if _, ok := r.Lookup("n1"); !ok {
		t.Fatal("the local node stays")
	}
	if r.RemoveRemote("n1", "server-b"); true {
		if e, _ := r.Lookup("n1"); e != local {
			t.Fatal("RemoveRemote never removes a local node")
		}
	}
}
