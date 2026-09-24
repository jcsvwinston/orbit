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
			want: present, note: "servers configured as peers (server.Config.PeerAddrs) keep one stream to each other and announce " +
				"their nodes; B lists A's node as remote with the origin in the label orbit.server, and refuses a Data Studio " +
				"request for it naming the owner (ADR-014).",
			probe: probeSharedRegistry},
		{id: "HA-03", family: "ha", title: "a UI subscribed on server B receives events from an agent connected to A",
			want: present, note: "an event a local agent sends is relayed to every peer, which publishes it to its own UI " +
				"subscribers and its replay ring; while a peer is connected the agents ship everything, since the mesh carries " +
				"events, not the peers' filters (ADR-014).",
			probe: probeCrossServerEvents},
		{id: "HA-04", family: "ha", title: "agents are assigned across servers deterministically",
			want: present, note: "with server.Config.AssignNodes each node is owned by one server, chosen by rendezvous hashing over " +
				"this server and the peers it reaches; an agent that registers elsewhere is sent Command.redirect and reconnects to " +
				"its owner, which it accepts only for an endpoint it is configured for.",
			probe: probeAgentSharding},
		{id: "HA-05", family: "ha", title: "a reconnect under the same node_id supersedes the previous stream: one node, no duplicates",
			want: present, note: "the registry keeps one entry and the superseded stream is ENDED: the handler waits on its context " +
				"as well as on Receive, returns Aborted to the old peer when a newer registration evicts it, and drops a frame that " +
				"raced in after (OR-56).",
			probe: probeSameNodeIDSupersedes},
	}
}
