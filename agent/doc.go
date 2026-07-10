// Package agent embeds in every Nucleus framework process and connects to a
// standalone admin server (see ../server) to ship observability events.
//
// The module is implemented: the agent loop (Run), the per-kind drop-oldest
// ring buffers, the endpoint-failover dialer with exponential backoff, the
// bidi stream lifecycle, and the admin_agent_* Prometheus collectors live in
// the sub-packages listed in README.md.
//
// Hard architectural invariants for the agent:
//
//   - The framework's hot path must NEVER block on, fail because of, or wait
//     for the agent. The agent is a strictly opt-in observer.
//   - When no operator is subscribed, the agent's per-event-type atomic
//     counter is zero and the agent is a no-op. Constructing an event must be
//     gated on that counter at the call site.
//   - The agent dials the admin server, never the other way around. This
//     makes NAT/firewall traversal symmetric between on-prem and cloud
//     deployments.
//   - The connection list (ExtensionConfig.Endpoints) is tried in order
//     with health-check; only after every endpoint fails does the agent enter
//     exponential backoff (cap 30s, jitter).
//   - Events that cannot be shipped are dropped (drop-oldest per type) and
//     reported via the admin_agent_events_dropped_total Prometheus counter.
//     The framework never persists telemetry; that is OpenTelemetry's job.
package agent
