// Package server implements the standalone Nucleus admin observability
// server. It accepts AgentService streams from agents (one per framework
// process) and ControlService unary/server-streaming calls from the embedded
// web UI.
//
// The module is implemented: the connection registry (nodes), the fanout and
// replay routing primitives (routing/*), the auth middlewares, and the
// Connect-RPC services live in the sub-packages listed in README.md.
//
// Architecture invariants for the server:
//
//   - The admin server is single-instance by default. Active-passive failover
//     is supported by configuring multiple endpoints in the agents'
//     ExtensionConfig.Endpoints list. Active-active is documented as a future
//     extension but is NOT implemented.
//
//   - The server NEVER calls back into agents over a separate connection. All
//     server-to-agent traffic (Subscribe, Unsubscribe, SnapshotRequest)
//     travels on the existing AgentService.Stream multiplexed Frame channel.
//
//   - The server retains only what it is asked to: without Config.DataDir
//     events live in bounded ring buffers, dropped on overflow, and a
//     restart starts empty. With a data directory it keeps events, the
//     fleet audit trail and host-metrics samples in one local SQLite file
//     for a retention window (ADR-013). Long-term storage is still
//     OpenTelemetry's job.
//
//   - A shared token and/or mutual TLS (client certificates verified
//     against --agent-client-ca) gate the agent listener; trusted-proxy
//     headers (X-Auth-User, X-Auth-Email) plus an optional bearer fallback
//     gate the UI listener. The server is never exposed through the
//     application's public load balancer.
package server
