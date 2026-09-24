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
			want: present, note: "server.Config.AlertRules (--alert-rules-file) are evaluated against every heartbeat's host metrics " +
				"(server/alerts): a rule fires once its condition has held for `for` on a node it applies to, resolves when it stops, " +
				"and the alert names the rule, the node, the value and the time. The probe configures a rule every node breaches and reads the alert.",
			probe: probeThresholdRules},
		{id: "ALR-02", family: "alerts", title: "alert channels (webhook, e-mail) exist",
			want: present, note: "channels are server configuration: webhooks by name (--alert-webhooks name=url, a JSON POST) and one " +
				"e-mail channel (--alert-smtp-*); a rule names the channels it notifies on firing and on resolution. The probe stands up " +
				"a webhook and receives the alert.",
			probe: probeAlertChannels},
		{id: "ALR-03", family: "alerts", title: "the UI API exposes alert state",
			want: present, note: "AlertService on the UI listener, behind the UI auth chain: ListAlertRules returns the rules as " +
				"configured, ListAlerts the firing alerts (and the resolved ones on request), StreamAlerts every state change.",
			probe: probeAlertStateInAPI},
		{id: "ALR-04", family: "alerts", title: "the server publishes its own Prometheus collectors (nodes connected, events dropped) on the metrics listener",
			want: present, note: "the metrics listener serves the server's own registry beside the default one: admin_server_nodes_connected, " +
				"nodes_known, frames/events/heartbeats received, Data Studio requests by outcome, alerts firing/fired/resolved, replay " +
				"buffered events, events published/dropped to UI subscriptions.",
			probe: probeServerOwnMetrics},
		{id: "ALR-05", family: "alerts", title: "the agent publishes its own Prometheus collectors on its metrics listener",
			want: present, probe: probeAgentOwnMetrics},
		{id: "ALR-06", family: "alerts", title: "a node that stops sending frames is listed as not connected within the inactivity timeout plus one janitor tick",
			want: present, probe: probeStaleNodeMarked},
	}
}
