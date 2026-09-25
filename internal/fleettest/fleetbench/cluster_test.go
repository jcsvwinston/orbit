// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"sort"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/db"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
)

// The gate of arc A9 asks one more thing than the fifty controls: that what
// the panel does for ONE application (the A6 parity checklist) the fleet
// does for a CLUSTER — three agents behind two servers — and does it from
// either server. Every control in the bench measures one server and one or
// two agents; none of them puts three nodes on two servers and asks the
// same questions of both sides. This test does, and the CI test lane runs
// it with the rest of internal/fleettest (the umbrella's fleet-posture
// guard checks that it is still here and still run).
//
// The cluster: servers A and B peered; agents a1 and a2 connected to A, a3
// connected to B; each agent registers TestArticle from a database of its
// own. What parity means here, item by item:
//
//   - inventory: each server lists all three nodes, names which server a
//     remote node is connected through, and lists a local one as its own;
//   - events: a UI subscribed on either server receives what each of the
//     three agents emits;
//   - Data Studio: each node's models and records are reachable through
//     the server it is connected to, and a request for it on the OTHER
//     server is refused naming the owner rather than answered wrongly;
//   - audit: a mutation on a node is attributed to that node on the server
//     that performed it;
//   - liveness: an agent that stops is listed as not connected on both
//     servers.

type clusterNode struct {
	ag   *runningAgent
	home *runningServer
	away *runningServer
	// homeID is the ServerID of home; a remote entry names it.
	homeID string
}

// threeAgentCluster boots two peered servers that allow mutations on
// TestArticle, and three agents with their own databases: two behind the
// first server, one behind the second. It returns once every node is
// registered locally where it connected and visible remotely on the other
// server.
func (e *env) threeAgentCluster(t *testing.T) (a, b *runningServer, nodes []clusterNode) {
	t.Helper()
	pa, pb := freePort(t), freePort(t)
	epA, epB := "http://127.0.0.1:"+pa, "http://127.0.0.1:"+pb
	mk := func(id, self, addr, peer string) server.Config {
		return server.Config{
			AgentAddr:               addr,
			ServerID:                id,
			AgentAdvertiseAddr:      self,
			PeerAddrs:               []string{peer},
			DataStudioAllowedModels: []string{"TestArticle"},
		}
	}
	a = e.startServer(t, mk("fb-cluster-a", epA, "127.0.0.1:"+pa, epB))
	b = e.startServer(t, mk("fb-cluster-b", epB, "127.0.0.1:"+pb, epA))
	if !pollUntil(5*time.Second, func() bool {
		return len(a.State().Peers.LivePeers()) == 1 && len(b.State().Peers.LivePeers()) == 1
	}) {
		t.Fatalf("the two servers did not peer in 5s (A sees %v, B sees %v)", a.State().Peers.LivePeers(), b.State().Peers.LivePeers())
	}
	start := func(id string, home, away *runningServer, homeID, ep string) clusterNode {
		d, reg := e.agentDB(t, false)
		ag := e.startAgent(t, agent.Config{
			Endpoints:      []string{ep},
			NodeIDOverride: id,
			Registry:       reg,
			Databases:      map[string]*db.DB{"default": d},
		})
		return clusterNode{ag: ag, home: home, away: away, homeID: homeID}
	}
	nodes = []clusterNode{
		start("fb-cluster-a1", a, b, "fb-cluster-a", epA),
		start("fb-cluster-a2", a, b, "fb-cluster-a", epA),
		start("fb-cluster-b3", b, a, "fb-cluster-b", epB),
	}
	for _, n := range nodes {
		if !waitRegistered(n.home.Server, n.ag.NodeID(), 4*time.Second) {
			t.Fatalf("%s did not register with %s", n.ag.NodeID(), n.homeID)
		}
	}
	if !pollUntil(5*time.Second, func() bool {
		for _, n := range nodes {
			en, ok := n.away.State().Nodes.Lookup(n.ag.NodeID())
			if !ok || !en.Remote() || en.Info.Via != n.homeID {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("the peers did not share the three nodes in 5s: A holds %v, B holds %v",
			describe(a.State().Nodes.List()), describe(b.State().Nodes.List()))
	}
	return a, b, nodes
}

func TestFleetParityThreeAgents(t *testing.T) {
	e := newEnv(t)
	a, b, cluster := e.threeAgentCluster(t)
	ctx := ctxFor(t)

	want := make([]string, 0, len(cluster))
	for _, n := range cluster {
		want = append(want, n.ag.NodeID())
	}
	sort.Strings(want)

	// ---- inventory: both servers list the three nodes and say whose each is.
	for _, srv := range []*runningServer{a, b} {
		resp, err := e.control(srv.Server).ListNodes(ctx, connect.NewRequest(&adminv1.ListNodesRequest{}))
		if err != nil {
			t.Fatalf("ListNodes on %s: %v", srv.State().ServerID, err)
		}
		var got []string
		for _, info := range resp.Msg.GetNodes() {
			got = append(got, info.GetNodeId())
			if !info.GetConnected() {
				t.Errorf("%s lists %s as not connected", srv.State().ServerID, info.GetNodeId())
			}
			via := info.GetLabels()["orbit.server"]
			home := homeOf(cluster, info.GetNodeId())
			if home == srv.State().ServerID && via != "" {
				t.Errorf("%s lists its own node %s as connected through %q", srv.State().ServerID, info.GetNodeId(), via)
			}
			if home != srv.State().ServerID && via != home {
				t.Errorf("%s lists %s connected through %q, want %q", srv.State().ServerID, info.GetNodeId(), via, home)
			}
		}
		sort.Strings(got)
		if !equalStrings(got, want) {
			t.Fatalf("%s lists %v, want %v", srv.State().ServerID, got, want)
		}
	}

	// ---- events: a subscriber on either server hears every agent.
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	liveA, _ := subscribeHTTP(ctxA, e.control(a.Server), false)
	liveB, _ := subscribeHTTP(ctxB, e.control(b.Server), false)
	if !pollUntil(3*time.Second, func() bool {
		return a.State().EventBus.SubscriberCount() >= 1 && b.State().EventBus.SubscriberCount() >= 1
	}) {
		t.Fatal("the UI subscriptions never registered on the servers' buses")
	}
	for _, n := range cluster {
		if !waitDemand(n.ag.bus, 3*time.Second) {
			t.Fatalf("no demand reached %s's bus", n.ag.NodeID())
		}
	}
	wantPaths := make([]string, 0, len(cluster))
	for _, n := range cluster {
		p := "/parity/" + n.ag.NodeID()
		wantPaths = append(wantPaths, p)
		emitHTTP(n.ag.bus, n.ag.NodeID(), p)
	}
	sort.Strings(wantPaths)
	for name, ch := range map[string]<-chan *adminv1.Event{"A": liveA, "B": liveB} {
		got := collectPaths(ch, len(cluster), 4*time.Second)
		sort.Strings(got)
		if !equalStrings(got, wantPaths) {
			t.Errorf("the subscriber on %s saw %v, want %v", name, got, wantPaths)
		}
	}

	// ---- Data Studio: each node through its own server; refused on the other.
	for _, n := range cluster {
		id := n.ag.NodeID()
		models, err := e.dataStudio(n.home.Server).ListModels(ctx, connect.NewRequest(&adminv1.ListModelsRequest{NodeId: id}))
		if err != nil {
			t.Fatalf("ListModels for %s on %s: %v", id, n.homeID, err)
		}
		if !hasModel(models.Msg.GetModels(), "TestArticle") {
			t.Errorf("%s does not list TestArticle for %s", n.homeID, id)
		}
		if models.Msg.GetNodeId() != id {
			t.Errorf("the models for %s were answered by %q", id, models.Msg.GetNodeId())
		}
		if _, err := e.dataStudio(n.home.Server).CreateRecord(ctx, connect.NewRequest(&adminv1.CreateRecordRequest{
			NodeId:    id,
			ModelName: "TestArticle",
			Record:    &adminv1.Record{ValuesJson: map[string]string{"Title": `"` + id + `"`, "Body": `"parity"`}},
		})); err != nil {
			t.Fatalf("CreateRecord for %s on %s: %v", id, n.homeID, err)
		}
		// The record landed on THAT node's database: the page answered for
		// the node names it and holds the title, and nobody else's does.
		page, err := e.dataStudio(n.home.Server).ListRecords(ctx, connect.NewRequest(&adminv1.ListRecordsRequest{
			NodeId: id, ModelName: "TestArticle", Page: 1, PageSize: 10,
		}))
		if err != nil {
			t.Fatalf("ListRecords for %s on %s: %v", id, n.homeID, err)
		}
		if page.Msg.GetNodeId() != id {
			t.Errorf("the page for %s was answered by %q", id, page.Msg.GetNodeId())
		}
		titles := titlesOf(page.Msg.GetItems())
		if !containsString(titles, id) {
			t.Errorf("%s does not hold the record created on it: %v", id, titles)
		}
		for _, other := range cluster {
			if other.ag.NodeID() != id && containsString(titles, other.ag.NodeID()) {
				t.Errorf("%s holds a record created on %s: the servers mixed the nodes' databases", id, other.ag.NodeID())
			}
		}
		_, err = e.dataStudio(n.away.Server).ListModels(ctx, connect.NewRequest(&adminv1.ListModelsRequest{NodeId: id}))
		if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("a Data Studio request for %s on the other server answered %v, want FailedPrecondition naming %s", id, err, n.homeID)
		}
	}

	// ---- audit: the server that performed a mutation attributes it to the node.
	for _, srv := range []*runningServer{a, b} {
		resp, err := e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
		if err != nil {
			t.Fatalf("ListAudit on %s: %v", srv.State().ServerID, err)
		}
		audited := map[string]bool{}
		for _, en := range resp.Msg.GetEntries() {
			if en.GetAction() == "datastudio.create" && en.GetActor() == operatorName {
				audited[en.GetNodeId()] = true
			}
		}
		for _, n := range cluster {
			if n.homeID != srv.State().ServerID {
				continue
			}
			if !audited[n.ag.NodeID()] {
				t.Errorf("%s has no create audited on %s", srv.State().ServerID, n.ag.NodeID())
			}
		}
	}

	// ---- liveness: an agent that stops is not connected on either server.
	// A clean stop sends Goodbye and the home server drops the entry; the
	// peer drops its remote copy. Either an absent node or one listed as
	// not connected is the answer an operator needs; a node still listed
	// as connected on either server is not.
	gone := cluster[2]
	gone.ag.stop()
	for _, srv := range []*runningServer{a, b} {
		if !pollUntil(4*time.Second, func() bool {
			resp, err := e.control(srv.Server).ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
			if err != nil {
				return false
			}
			for _, info := range resp.Msg.GetNodes() {
				if info.GetNodeId() == gone.ag.NodeID() {
					return !info.GetConnected()
				}
			}
			return true
		}) {
			t.Errorf("%s still lists %s as connected 4s after it stopped", srv.State().ServerID, gone.ag.NodeID())
		}
	}
}

func homeOf(cluster []clusterNode, nodeID string) string {
	for _, n := range cluster {
		if n.ag.NodeID() == nodeID {
			return n.homeID
		}
	}
	return ""
}

func titlesOf(records []*adminv1.Record) []string {
	var out []string
	for _, r := range records {
		out = append(out, unquote(r.GetValuesJson()["Title"]))
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasModel(models []*adminv1.ModelInfo, name string) bool {
	for _, m := range models {
		if m.GetName() == name {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
