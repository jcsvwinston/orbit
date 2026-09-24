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
			want: present, note: "with server.Config.DataDir the server retains events in a SQLite file (server/store) and warms " +
				"its replay ring from it at start; a restarted server replays what the previous process received. Without a data " +
				"directory the ring is in memory and a restart starts empty, as before.",
			probe: probeReplaySurvivesRestart},
		{id: "RET-02", family: "retention", title: "an event emitted while the agent has no stream is delivered after it reconnects",
			want: present, note: "when a stream ends the agent keeps listening to the bus under the filters the server had and " +
				"parks the events in its ring buffer (agent/catcher.go); the next stream drains the buffer right after registering. " +
				"Bounded by the buffer: an outage longer than it keeps the newest events per kind, counted as dropped.",
			probe: probeOfflineEventsDelivered},
		{id: "RET-03", family: "retention", title: "host metrics have a history per node, not only the last sample",
			want: present, note: "with a data directory the server retains one host-metrics sample per heartbeat and node and " +
				"MetricsService.ListHostMetrics returns them oldest first, within the retention window and since the instant asked; " +
				"without one it answers an empty list and ListNodes carries the last sample only.",
			probe: probeHostMetricsHistory},
		{id: "RET-04", family: "retention", title: "the fleet audit trail survives a server restart",
			want: present, note: "with server.Config.DataDir every audit entry is written to the store and ListAudit reads from it, " +
				"so a restarted server serves the trail the previous process wrote, within the retention window.",
			probe: probeAuditSurvivesRestart},
		{id: "RET-05", family: "retention", title: "a retention window is configurable and enforced",
			want: present, note: "server.Config.Retention (--retention, default 7 days, with --data-dir) bounds every read and a " +
				"janitor deletes older rows; the probe sets a one-second window and watches an audit entry leave what the server serves.",
			probe: probeRetentionWindow},
		{id: "RET-06", family: "retention", title: "the fleet audit trail can be exported (CSV or JSON download)",
			want: present, note: "GET /api/audit/export?format=csv|json on the UI listener, behind the same auth chain as the RPCs, " +
				"downloads the trail ListAudit serves (the store when the server retains, the ring otherwise), newest first, up to 10000 rows.",
			probe: probeAuditExport},
		{id: "RET-07", family: "retention", title: "the replay buffer is bounded, drops the oldest and exposes its size and counters",
			want: present, probe: probeReplayBounded},
	}
}
