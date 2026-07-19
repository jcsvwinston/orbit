---
title: Security
sidebar_position: 2
description: The fleet plane's two auth surfaces, and how to harden them.
---

# Security

The admin server exposes two listeners, and each has its own authentication
surface. This page covers both, plus what the deliberate exceptions imply.
(For the in-process panel's own login, sessions and RBAC, see
[How it works](../how-it-works.md) — this page is about the standalone
fleet plane.)

## Two auth planes

### Operators (the UI listener)

The server does **not** implement OIDC or password login on the fleet UI.
The canonical deployment puts an auth-aware reverse proxy (oauth2-proxy,
nginx `auth_request`, Traefik forward-auth) in front of `--ui-addr`, and the
proxy forwards the authenticated identity in headers. A request is
authenticated as an operator when **either** of these succeeds, checked in
this order:

1. **Trusted-proxy headers.** The request comes from an IP inside
   `--ui-trusted-cidrs` (default: loopback only), carries a non-empty
   identity header (`X-Auth-User` by default; rename with
   `--ui-auth-header`), and — when `--ui-proxy-secret` is set — echoes that
   secret in the fixed `X-Auth-Proxy-Secret` header. Optional headers carry
   the email (`X-Auth-Email`) and the role (`X-Auth-Role`).
2. **Bearer fallback.** The request carries
   `Authorization: Bearer <token>` matching `--ui-bearer`. Useful without a
   proxy (development, small trusted networks). When `--ui-bearer` is
   empty, the fallback is disabled.

Failures get a generic `401` — the response does not reveal which
credential mode was attempted or why it failed.

**Set `--ui-proxy-secret` in production.** Without it, any process that can
source packets from a trusted CIDR (a sidecar, a host-networked container,
another local process behind the same NAT) can forge an operator identity
just by setting the header. The secret is compared in constant time, and a
request that fails the secret check falls through to the bearer path rather
than being rejected outright — a misconfigured proxy never blocks a valid
bearer.

### Agents (the agent listener)

Agents authenticate with a single shared bearer token: `--agent-token` on
the server, `Token` in the agent's configuration. The agent attaches it to
**every** call, including the long-lived telemetry stream itself. Token
comparison is constant-time.

The server is **fail-closed** here: it refuses to start the agent listener
on a non-loopback interface with no token and no TLS. The
`--insecure-agent-listener` override exists for networks where a firewall,
private subnet, or service-mesh mTLS already restricts reachability — and
the server logs a warning at boot when you use it. Treat access to the
agent listener as fleet-write access: an unauthenticated one would let any
host on the network register as an agent and feed the fleet plane.

## The /healthz exemption

`/healthz` answers `200 ok` on **both** listeners (and on the metrics
listener) **without authentication**. That is intentional — load balancers
and the agent's endpoint-failover dialer need to probe reachability without
owning a token — but it has two consequences worth knowing:

- Anyone who can reach a listener can learn that an Orbit admin server is
  running there. Nothing else is exposed without credentials.
- **Reachable is not authenticated.** The agent's dial probe hits
  `/healthz`, so a booting agent can find the server "reachable" while its
  token is being rejected on the stream. The boot-time `RequireConnection`
  gate does **not** trust that probe: it only passes once the admin server
  accepts the agent's first stream frame under authentication — so a wrong
  token fails the application's boot within the configured deadline, with
  the token-rejected warnings described below explaining why.

## Read-only operators

Two mechanisms, verified in the auth chain on every request:

- **Per operator:** the trusted proxy sets the role header
  (`X-Auth-Role: viewer` — `readonly` and `read-only` also work,
  case-insensitively). That operator can use every read surface, but Data
  Studio mutations (create, update, delete, bulk) are refused. Any other
  value, including no header, keeps the operator read-write.
- **Globally:** `--ui-read-only` marks **every** operator read-only,
  turning the server into a pure observability plane.

By default, any read-write operator can run every Data Studio mutation on
every model of every connected node — the fleet Access control screen is a
read-only snapshot of each node's policy, not a per-verb gate on the
operator. Mutations are attributed and recorded in the server's fleet
Audit log, but if some operators should not write, scope them down with
the role header or run the whole server read-only.

## Credential lockout

Both listeners keep a small per-IP lockout: **20 wrong credentials within a
minute** lock that IP out (`429 Too Many Requests`) until the window
expires. Only requests that actually **presented** a wrong credential — a
bad bearer, or a wrong proxy secret alongside a bearer attempt — count;
credential-less requests (a browser hitting the SPA before signing in)
never do, so nobody can lock operators out by poking the login page. The
limiter exists to make online brute force of the shared tokens
impractical; it is not a general-purpose WAF.

## Rejected tokens are loud

A bad agent token used to be easy to miss, so both sides now warn:

- **Server side:** a warning naming the remote IP —
  `admin server rejected agent request: invalid or missing bearer token` —
  rate-limited to one per minute per IP, with a count of the rejections
  suppressed in between.
- **Agent side:** `admin agent token rejected by admin server; check
  --agent-token`, at most once per minute per endpoint.
- **Backoff:** the agent's reconnect backoff only resets after the server
  has demonstrably **accepted** the stream (first frame received). A
  rejected token therefore retries at growing intervals up to 30 seconds —
  not once per second forever — and the agent's `connected` log line is
  only emitted on real acceptance.

## Browser-facing headers

Every response on the UI listener carries:

- `Content-Security-Policy: default-src 'self'; script-src 'self';
  style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self';
  connect-src 'self'; frame-ancestors 'none'; base-uri 'none';
  form-action 'self'` — the SPA is fully self-contained, so a strict CSP
  costs nothing (`'unsafe-inline'` is needed for styles only).
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`

## TLS

Both listeners speak HTTP/2: cleartext (h2c) by default, TLS 1.2+ when a
PEM pair is supplied (`--agent-cert`/`--agent-key`,
`--ui-cert`/`--ui-key`). Typical production setups either terminate TLS at
the reverse proxy (UI listener) and give the agent listener its own
certificate, or keep both listeners on a private, mesh-encrypted network.
Agents accept `https://` endpoints and use the system trust store.

## The metrics listener

`--metrics-addr` (empty by default — disabled) serves Prometheus
`/metrics` and `/healthz` **without authentication, by design**. Bind it to
a private interface (`127.0.0.1:9091`, a node-internal address), exactly as
you would any metrics port.

## Hardening checklist

- [ ] `--agent-token` set (or agent-listener TLS), and **not** passed on
      the command line in production — use `NUCLEUS_ADMIN_AGENT_TOKEN`
      from a root-only environment file.
- [ ] `--insecure-agent-listener` **not** set.
- [ ] UI listener behind an SSO reverse proxy; `--ui-trusted-cidrs`
      narrowed to the proxy's real source range.
- [ ] `--ui-proxy-secret` set and echoed by the proxy in
      `X-Auth-Proxy-Secret`.
- [ ] `--ui-bearer` empty in proxy fronted setups (leave the fallback off
      unless you need it).
- [ ] Operators who only observe get `X-Auth-Role: viewer`; a pure
      observability deployment runs `--ui-read-only`.
- [ ] TLS on any listener that crosses a network you do not fully trust.
- [ ] `--metrics-addr` bound to a private interface, or left disabled.
- [ ] Log pipeline alerts on the two token-rejected warnings and on the
      boot warning about an exposed agent listener.
