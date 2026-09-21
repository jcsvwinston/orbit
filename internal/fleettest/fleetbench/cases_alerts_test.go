// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsAlerts is the family that asks whether the server tells someone.
// It exposes what it observes — the agent's own collectors, a node marked
// stale — and evaluates nothing: there is no rule, no channel and no alert
// state anywhere on the wire or in the configuration.
func controlsAlerts() []control {
	return []control{
		{id: "ALR-01", family: "alerts", title: "a threshold rule on a host metric raises an alert",
			want: absent, note: "server.Config has no rule or threshold field and admin.proto declares no rule, threshold or " +
				"alert message: the server stores the last HostMetrics sample per node and evaluates nothing against it.",
			probe: probeThresholdRules},
		{id: "ALR-02", family: "alerts", title: "alert channels (webhook, e-mail) exist",
			want: absent, note: "server.Config has no webhook, SMTP or notification field and the protocol has no channel or " +
				"recipient message: nothing on the server can be told where to send anything.",
			probe: probeAlertChannels},
		{id: "ALR-03", family: "alerts", title: "the UI API exposes alert state",
			want: absent, note: "ControlService and ManageService (admin.proto) have no RPC and no message about alerts or incidents; " +
				"the fleet UI has nothing to show.",
			probe: probeAlertStateInAPI},
		{id: "ALR-04", family: "alerts", title: "the server publishes its own Prometheus collectors (nodes connected, events dropped) on the metrics listener",
			want: absent, note: "the metrics listener (server.Config.MetricsAddr) serves the default registry only — go_* and " +
				"process_* families; server.go mounts promhttp.Handler() and registers no collector of its own, which " +
				"server/config.go calls future work.",
			probe: probeServerOwnMetrics},
		{id: "ALR-05", family: "alerts", title: "the agent publishes its own Prometheus collectors on its metrics listener",
			want: present, probe: probeAgentOwnMetrics},
		{id: "ALR-06", family: "alerts", title: "a node that stops sending frames is listed as not connected within the inactivity timeout plus one janitor tick",
			want: present, probe: probeStaleNodeMarked},
	}
}
