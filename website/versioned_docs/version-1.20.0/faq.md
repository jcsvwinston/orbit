---
title: FAQ & troubleshooting
sidebar_position: 9
description: Answers to the questions operators actually hit.
---

# FAQ & troubleshooting

Every answer below describes the shipped behaviour of the current release. The
first question decides whether the rest of the page concerns you: almost all of
it is about the standalone fleet plane, not the in-process panel.

## Do I need the fleet plane at all?

**Probably not.** The in-process panel (`orbit.Module`) is complete on its own,
and a multi-node application can share one live feed with just the Redis relay
(the `cluster_*` keys in [Configuration](./configuration.md)).

Deploy the [agent/server fleet](./cluster/overview.md) only when you want a
dedicated, always-on observability server that outlives any single application
node.

## The server logs "rejected agent request: invalid or missing bearer token"

**Cause:** an agent — or something else — is calling the agent listener with a
wrong or absent token. Check, in order:

1. The agent's `Token` equals the server's `--agent-token` exactly. Both sides
   trim surrounding whitespace; nothing else is forgiven.
2. Which source actually set the server's token. Flags beat the
   `NUCLEUS_ADMIN_AGENT_TOKEN` environment variable, which beats defaults. A
   stale value in a unit file or container environment is the classic cause.
3. How many attempts you are really seeing. The warning names the remote IP and
   is rate-limited to one per minute per IP, with a count of the rejections
   suppressed in between — so one line can represent many attempts.

## The agent logs "admin agent token rejected by admin server"

**Cause:** the same token mismatch, seen from the agent's side. It is also
rate-limited, once per minute per endpoint.

Nothing else breaks meanwhile: the agent keeps retrying with exponential
backoff up to 30 seconds, and it does not log `connected` until the server
actually accepts the stream. Fix the token and the next attempt succeeds
without a restart.

## The agent logs "consecutive stream cycles ended without a single accepted frame"

**Cause:** almost certainly the same bad token, inferred rather than reported.
Some transport failures do not carry the authentication error cleanly, so after
three consecutive stream cycles against one endpoint in which the server never
accepted a single frame, the agent raises this warning even without an explicit
rejection.

Two things it deliberately does not do:

- Unreachable endpoints never trigger it. Only cycles that connect and then die
  frameless count.
- With several configured endpoints, each is counted on its own. The warning
  names the endpoint whose cycles actually died, and an accepted frame clears
  only that endpoint's count.

If it persists, verify `--agent-token` on both sides.

## /healthz answers, but no node ever appears

**Cause:** the token is being rejected. `/healthz` is deliberately **exempt
from authentication** on every listener, so it proves reachability and nothing
more — a reachable server plus no registered node is the signature of a bad
token. Check the warnings above.

The agent's boot-time `RequireConnection` gate is **not** satisfied by
reachability either: it waits for the server to accept the stream under
authentication. With a wrong token the boot fails at the deadline, and the
warnings tell you why. See
[Security](./operations/security.md#the-healthz-exemption).

## A node keeps flipping between connected and offline

**Cause:** frames are not arriving within the server's inactivity window. The
server marks a node **stale** when no frame — event or heartbeat — arrives
within that window (default 45 seconds, sized for the agent's default
10-second heartbeat), and the next frame flips it back.

Usual culprits:

- something between agent and server is silently dropping the long-lived
  stream (aggressive idle timeouts on a proxy or NAT);
- the process is being paused (CPU starvation, stop-the-world suspends);
- a custom `HeartbeatInterval` was raised close to or beyond the server's
  window.

The registry entry is never evicted on staleness, so history and identity
survive the flapping while you fix the cause.

## Operators get 401 behind the reverse proxy

**Cause:** one of the three trusted-proxy conditions is not met — identity
headers are honoured only when all of them match. Check:

1. The proxy's source IP is inside `--ui-trusted-cidrs`. The default trusts
   loopback **only**, which breaks the moment the proxy moves to another host.
2. The proxy sends a non-empty identity header, `X-Auth-User` unless you
   changed `--ui-auth-header`.
3. If `--ui-proxy-secret` is set, the proxy echoes it in
   `X-Auth-Proxy-Secret` on **every** upstream request.

The `401` is deliberately generic and will not tell you which of the three
failed. In most cases the fix is in the proxy config, not the server.

## Suddenly everything answers 429 "too many failed attempts"

**Cause:** the per-IP lockout tripped — 20 wrong credential presentations
within a minute from one IP. It clears itself when the window expires, in up to
a minute.

Do not just wait it out. Only requests that presented a wrong credential count,
and unauthenticated page loads never do, so a `429` means something at that IP
is actively sending bad tokens. Find it: a misconfigured agent, or a dashboard
poller with an old bearer.

## Data Studio buttons are missing or every write is refused

**Cause:** that operator is read-only, and both ways of arriving there are
deliberate configurations rather than errors. Either the proxy sends
`X-Auth-Role: viewer` (or `readonly` / `read-only`) for them, or the server
runs with `--ui-read-only`, which makes **every** operator read-only. See
[Security](./operations/security.md#read-only-operators).

## Which ports need to be open?

- **App nodes → server:** the agent listener (`--agent-addr`, default
  `:9090`). Agents dial out; nothing dials into your app nodes.
- **Browsers/proxy → server:** the UI listener (`--ui-addr`, default
  `:8080`).
- **Prometheus → server:** the opt-in metrics listener
  (`--metrics-addr`), private interface only.

Both main listeners speak HTTP/2 — cleartext h2c, or TLS when configured — and
the agent stream is a single long-lived HTTP/2 connection. Any proxy in the
path must support HTTP/2 end to end and tolerate long-lived streams.

## Where do I see metrics?

- **Server:** set `--metrics-addr` (for example `127.0.0.1:9091`) and
  scrape `/metrics` — currently the Prometheus default registry (`go_*`,
  `process_*` collectors). Unauthenticated by design; keep it private.
- **Agent:** set `MetricsAddr` in the agent's configuration for a
  standalone `/metrics` endpoint with the `admin_agent_*` collectors
  (connection state, buffer sizes, reconnect counts), or call
  `Agent.Metrics()` and serve them from your app's own metrics endpoint.

## An agent is connected, but the live streams look empty

**Usually working as designed.** Producers short-circuit when nobody is
watching: events only flow while a fleet UI holds an open subscription, so an
idle server receives heartbeats, not traffic.

Open a stream page and you get the recent-history replay, which is bounded per
event kind and drops oldest first — a live operational view, not a persistent
store. Brief disconnects on the agent side are bridged by ring buffers
(defaults: 256 HTTP, 256 SQL, 64 session, 64 custom events), which also drop
oldest first under pressure.

## Can agents and the server run different versions?

**Within reason.** The wire contract is append-only inside its v1 package and
written for rolling deploys, so an agent one release behind a newer server is
the expected case.

There is no version handshake, so do not let the gap grow. Upgrade the server
first, then roll the agents, as described in
[Upgrading](./operations/upgrade.md).
