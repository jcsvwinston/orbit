---
title: FAQ & troubleshooting
sidebar_position: 9
description: Answers to the questions operators actually hit.
---

# FAQ & troubleshooting

Panel questions first, fleet-plane questions after. Every answer below
reflects the shipped behaviour of the current release.

## Do I need the fleet plane at all?

Probably not. The in-process panel (`orbit.Module`) is complete on its
own, and a multi-node app can aggregate its live feed with just the Redis
relay (`cluster_*` keys in [Configuration](./configuration.md)). Deploy the
[agent/server fleet](./cluster/overview.md) only when you want a
dedicated, always-on observability server that outlives any single
application node.

## The server logs "rejected agent request: invalid or missing bearer token"

An agent (or something else) is calling the agent listener with a wrong or
absent token. Check, in order:

1. The agent's `Token` value equals the server's `--agent-token` exactly
   (both sides trim surrounding whitespace, but nothing else is forgiven).
2. Which source actually set the server's token: flags beat the
   `NUCLEUS_ADMIN_AGENT_TOKEN` environment variable, which beats defaults.
   A stale value in a unit file or container environment is the classic
   cause.
3. The warning includes the remote IP, and is rate-limited to one per
   minute per IP with a count of rejections suppressed in between — one
   line per minute can represent many attempts.

## The agent logs "admin agent token rejected by admin server"

Same mismatch, seen from the agent's side (also rate-limited, once per
minute per endpoint). The agent keeps retrying with exponential backoff up
to 30 seconds — it does not give up, and it does not log `connected` until
the server actually accepts the stream. Fix the token; the next attempt
succeeds without a restart.

## /healthz answers, but no node ever appears

`/healthz` is deliberately **exempt from authentication** on every
listener, so it proves reachability and nothing more. A reachable server
plus no registered node almost always means the token is being rejected —
check both warnings above. Note that the agent's boot-time
`RequireConnection` gate is also satisfied by reachability, so a wrong
token will **not** fail your application's boot; the log warnings are the
signal. See [Security](./operations/security.md#the-healthz-exemption).

## A node keeps flipping between connected and offline

The server marks a node **stale** when no frame — event or heartbeat —
arrives within its inactivity window (default 45 seconds, sized for the
agent's default 10-second heartbeat). The next frame flips it back. If a
node oscillates:

- something between agent and server is silently dropping the long-lived
  stream (aggressive idle timeouts on a proxy or NAT);
- the process is being paused (CPU starvation, stop-the-world suspends);
- a custom `HeartbeatInterval` was raised close to or beyond the server's
  window.

The registry entry is never evicted on staleness, so history and identity
survive the flapping while you fix the cause.

## Operators get 401 behind the reverse proxy

The trusted-proxy path only honours identity headers when everything
matches. Check:

1. The proxy's source IP is inside `--ui-trusted-cidrs` — the default
   trusts loopback **only**, which breaks the moment the proxy moves to
   another host.
2. The proxy sends the identity header the server expects
   (`X-Auth-User` unless you changed `--ui-auth-header`), non-empty.
3. If `--ui-proxy-secret` is set, the proxy echoes it in
   `X-Auth-Proxy-Secret` on **every** upstream request.

The `401` is deliberately generic — it will not tell you which of the
three failed. Fix the proxy config, not the server, in most cases.

## Suddenly everything answers 429 "too many failed attempts"

The per-IP lockout tripped: 20 wrong credential presentations within a
minute from one IP. It clears itself when the window expires (up to a
minute). Only requests that presented a wrong credential count —
unauthenticated page loads never do — so a `429` means something at that
IP is actively sending bad tokens; find it (a misconfigured agent, a
dashboard poller with an old bearer) rather than waiting it out
repeatedly.

## Data Studio buttons are missing or every write is refused

That operator is read-only. Either the proxy sends
`X-Auth-Role: viewer` (or `readonly` / `read-only`) for them, or the
server runs with `--ui-read-only`, which makes **every** operator
read-only. Both are deliberate configurations, not errors — see
[Security](./operations/security.md#read-only-operators).

## Which ports need to be open?

- **App nodes → server:** the agent listener (`--agent-addr`, default
  `:9090`). Agents dial out; nothing dials into your app nodes.
- **Browsers/proxy → server:** the UI listener (`--ui-addr`, default
  `:8080`).
- **Prometheus → server:** the opt-in metrics listener
  (`--metrics-addr`), private interface only.

Both main listeners speak HTTP/2 (cleartext h2c, or TLS when configured),
and the agent stream is a single long-lived HTTP/2 connection — any proxy
in the path must support HTTP/2 end-to-end and tolerate long-lived
streams.

## Where do I see metrics?

- **Server:** set `--metrics-addr` (for example `127.0.0.1:9091`) and
  scrape `/metrics` — currently the Prometheus default registry (`go_*`,
  `process_*` collectors). Unauthenticated by design; keep it private.
- **Agent:** set `MetricsAddr` in the agent's configuration for a
  standalone `/metrics` endpoint with the `admin_agent_*` collectors
  (connection state, buffer sizes, reconnect counts), or call
  `Agent.Metrics()` and serve them from your app's own metrics endpoint.

## An agent is connected, but the live streams look empty

Working as designed, usually. Producers short-circuit when nobody is
watching: events only flow while a fleet UI holds an open subscription, so
an idle server receives heartbeats, not traffic. When you open a stream
page you get the recent-history replay, which is bounded per event kind
and drops oldest first — it is a live operational view, not a persistent
store. Brief disconnects on the agent side are bridged by ring buffers
(defaults: 256 HTTP, 256 SQL, 64 session, 64 custom events), which also
drop oldest first under pressure.

## Can agents and the server run different versions?

Within reason. The wire contract is append-only inside its v1 package and
written for rolling deploys, so an agent one release behind a newer server
is the expected case. There is no version handshake, so do not let the gap
grow — upgrade the server first, then roll the agents, as described in
[Upgrading](./operations/upgrade.md).
