// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsRetention is the family that asks what survives. The server
// documents itself as not being persistence (server/doc.go, server/README):
// events, the last metrics sample and the audit trail live in bounded
// memory and are gone with the process. The controls here measure that
// statement from outside — a second server on the same address, an event
// emitted into an outage — so that the day it stops being true, the bench
// says so.
func controlsRetention() []control {
	return []control{
		{id: "RET-01", family: "retention", title: "events the server replays to a new UI subscriber survive a server restart",
			want: absent, note: "the replay is a per-kind in-memory ring (server/routing/replay.go): a second server started on " +
				"the same address replays nothing to a new subscriber, and server.Config has no persistence knob to change that.",
			probe: probeReplaySurvivesRestart},
		{id: "RET-02", family: "retention", title: "an event emitted while the agent has no stream is delivered after it reconnects",
			want: absent, note: "the agent subscribes to the bus only while a stream is open and its ring buffer absorbs " +
				"backpressure on an OPEN stream (agent/buffer/buffer.go): three events emitted during a server outage never " +
				"arrive after the reconnect.",
			probe: probeOfflineEventsDelivered},
		{id: "RET-03", family: "retention", title: "host metrics have a history per node, not only the last sample",
			want: absent, note: "the node registry keeps the latest HostMetrics per node (server/nodes/registry.go SetHostMetrics); " +
				"NodeInfo carries one host_metrics message, not a series, and no RPC returns a history.",
			probe: probeHostMetricsHistory},
		{id: "RET-04", family: "retention", title: "the fleet audit trail survives a server restart",
			want: absent, note: "the fleet audit is an in-memory ring of 2048 entries (server/routing/audit.go AuditRing): after a " +
				"restart ListAudit is empty. The panel's own trail is a table with a retention window; the fleet's is not.",
			probe: probeAuditSurvivesRestart},
		{id: "RET-05", family: "retention", title: "a retention window is configurable and enforced",
			want: absent, note: "server.Config has no retention, TTL, persistence or data-directory field: there is nothing to " +
				"retain for and nothing to enforce it on.",
			probe: probeRetentionWindow},
		{id: "RET-06", family: "retention", title: "the fleet audit trail can be exported (CSV or JSON download)",
			want: absent, note: "no route on the UI listener answers an export path (the single-page fallback catches them) and " +
				"ManageService has only GetRbac and ListAudit; the panel's CSV export has no fleet counterpart.",
			probe: probeAuditExport},
		{id: "RET-07", family: "retention", title: "the replay buffer is bounded, drops the oldest and exposes its size and counters",
			want: present, probe: probeReplayBounded},
	}
}
