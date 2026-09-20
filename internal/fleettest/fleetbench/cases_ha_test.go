// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsHA is the family that asks what happens with more than one
// server. The agent side is ready for it — an ordered endpoint list, a
// reconnect that supersedes the old stream — and the server side is one
// process with one memory: two servers know nothing of each other's nodes
// or events, and nothing decides which agent goes where.
func controlsHA() []control {
	return []control{
		{id: "HA-01", family: "ha", title: "an agent fails over to the next endpoint when the first is unreachable",
			want: present, probe: probeEndpointFailover},
		{id: "HA-02", family: "ha", title: "two servers share the node registry: an agent connected to A is listed by B",
			want: absent, note: "each server keeps its own in-memory node registry (server/nodes/registry.go); server B lists " +
				"nothing about an agent connected to A. server/doc.go states active-active is not implemented.",
			probe: probeSharedRegistry},
		{id: "HA-03", family: "ha", title: "a UI subscribed on server B receives events from an agent connected to A",
			want: absent, note: "events fan out inside the server that received them (server/routing/eventbus.go) and the server " +
				"never talks to another server: a subscriber on B sees nothing an agent sends to A.",
			probe: probeCrossServerEvents},
		{id: "HA-04", family: "ha", title: "agents are assigned across servers deterministically",
			want: absent, note: "no shard, peer, cluster or assignment field in server.Config or agent.Config and nothing about it " +
				"on the wire: an agent connects to the first endpoint in its list that answers /healthz.",
			probe: probeAgentSharding},
		{id: "HA-05", family: "ha", title: "a reconnect under the same node_id supersedes the previous stream: one node, no duplicates",
			want: present, probe: probeSameNodeIDSupersedes},
	}
}
