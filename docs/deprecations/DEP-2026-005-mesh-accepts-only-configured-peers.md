# Behaviour-Change Notice: the mesh accepts only configured peers at v2.0.0

- ID: `DEP-2026-005`
- Status: `active`
- Announced in: the next minor after `v1.20.1` (A12 `M0-R`; the re-audit of
  2026-10-23 found it as OR-103, and the owner listed the flip for the
  major on 2026-10-27)
- Behaviour flip: `v2.0.0`, no earlier than 2027-02-07 — major-only per the
  suite's deprecation policy; the suite groups every breaking change into
  the one major at the close of its A12 arc (QADR-0010). This is a **change
  of what `PeerService.Sync` accepts**: nothing is removed, no
  `// Deprecated:` marker carries it.
- Scope: `behaviour`
- Affected lifecycle tag: `stable` (the mesh is what fleets of servers run;
  the `v0.x` tag of the `server` module does not exempt it from the window)
- Owner: `@jcsvwinston`

## Summary

Servers of a fleet speak to each other on their agent listeners: each one
dials the endpoints in its `--peers` and opens `PeerService.Sync`, and the
other side records what it hears — the peer's nodes in its registry, its
events in its bus and replay. Today the receiving side asks nothing beyond
the agent listener's own authentication, and `PeerService` is mounted on
every server, peers configured or not. A holder of the agent token, or of
any certificate the agents' CA signed, can open `Sync`, announce a server
that does not exist, and list nodes and relay events attributed to any
`node_id` to every operator of that server.

`server.Config.PeerAllowlist` (`--peer-allowlist`) closes it: a peer is
accepted only when it is a server this one dials, and under mutual TLS
only when its certificate names it. It is opt-in in `v1.x` because a
fleet whose servers do not all list each other would lose the links the
missing entries carry the day the default changed, and a published default
does not change within `v1.x`.

At the major every server applies the check.

What changes and when:

- **This release:** `server.Config.PeerAllowlist` (`--peer-allowlist`, env
  `NUCLEUS_ADMIN_PEER_ALLOWLIST=1`) is added. With it, `Sync` accepts a peer
  only when
  1. the `server_id` it announces is the one that answered on this
     server's own link to one of its `--peers` endpoints — the list names
     endpoints, and dialling one is what says which server sits behind it;
  2. when it arrived with a verified client certificate (an agent listener
     with `--agent-client-ca`), the certificate's Common Name is that
     `server_id`.

  Anything else is refused with `PermissionDenied` and a WARN naming the
  peer and the reason. A server with no `--peers` accepts no peer.
  Without the flag nothing is refused: each peer the check would refuse is
  accepted as before and logged **once per `server_id` per process** as a
  WARN that names this notice and the flag.
- **v2.0.0:** every server applies the check. `PeerAllowlist` and
  `--peer-allowlist` then describe what the server does anyway; they stay
  accepted, so a configuration that sets them keeps starting. There is no
  setting that keeps today's regime: a server that must be a peer is listed
  in `--peers`.

## Affected surfaces

- `PeerService.Sync` on the agent listener of every server.
- `server.Config.PeerAllowlist` — its effect becomes unconditional.
- The `admin-server` binary: `--peer-allowlist` /
  `NUCLEUS_ADMIN_PEER_ALLOWLIST`.
- Not affected: agents (`AgentService`), the UI listener, what a server
  sends down its own links, and a fleet whose servers list each other in
  `--peers` and, under mutual TLS, present certificates named after their
  `--server-id`.

## What changes at 2.0

| Peer that opens `Sync` | Today, default | Today, `--peer-allowlist` | At `v2.0.0` |
|---|---|---|---|
| a server this one dials (`--peers`), certificate named after it or no certificate | accepted | accepted | accepted (unchanged) |
| a server this one does not dial | accepted, WARN once | **refused** | **refused** |
| a caller with the agent token or an agent certificate, announcing any `server_id` | accepted, WARN once | **refused** — except a token holder announcing the name of a server this one dials (below) | **refused**, same exception |
| a certificate whose Common Name is not the announced `server_id` | accepted, WARN once | **refused** | **refused** |

Without client certificates the check compares names, not credentials: a
holder of the agent token who knows the `server_id` of a server this one
dials can still announce it. Under mutual TLS the certificate decides.

## Migration path

- **Now (v1):** make every link a configured one, then turn the check on.

  ```sh
  # each server lists every other server it exchanges nodes with
  admin-server --server-id a --peers https://b.internal:9090 ... --peer-allowlist
  admin-server --server-id b --peers https://a.internal:9090 ... --peer-allowlist
  ```

  Under mutual TLS, name each server's client certificate after its
  `--server-id` (or set `--server-id` to the certificate's Common Name).
  In Go: `server.Config{PeerAddrs: ..., PeerAllowlist: true}`.
- **At 2.0:** nothing more to do for a fleet that ran with the flag.
- **Behaviour differences, from the operator's side:** a server that one
  peer dials and that does not dial it back stops receiving that peer's
  nodes and events: list it in `--peers` on both sides. A peer refused
  logs `admin server: peer refused` with the reason on the refusing server,
  and the link fails with `PermissionDenied` on the dialling one.
- **Required changes:** at most one `--peers` entry per missing direction,
  and certificates named after the servers.

## Detection

- Per peer, once per process, on a server without the flag:
  `admin server: accepted a peer that Orbit 2.0 refuses` with `peer`,
  `agent_endpoint`, `certificate_cn` and `reason`. A server that never logs
  it is not affected.
- With the flag on, `admin server: peer refused` is what the major will
  refuse; a fleet can run with it in staging first.
- In configuration: `grep -E 'peer-allowlist|PEER_ALLOWLIST'` over the unit
  files and environment files of each `admin-server` with `--peers`.

The announcement is per peer rather than one WARN at start, unlike
`DEP-2026-001`: what changes depends on who connects, which a server does
not know until a peer does.

## What breaks if this is ignored

At `v2.0.0`, a server that receives a peer it does not dial, or whose
certificate names another server, refuses the link; the nodes and events
that peer carried disappear from that server's views. The server says
which peer and why on every refused attempt.

## Change window

- **Why `v2.0.0`:** a published default does not change within `v1.x`
  (QADR-0010); this one refuses links that are accepted today.
- **Why `2027-02-07`:** the suite's support policy sets a minimum of 90
  calendar days of notice, counted from the set that publishes the notice.
  This notice is dated 2026-11-03; the set expected to pin it first is the
  one of 2026-11-09, and 90 days later is 2027-02-07. If a later set is the
  first to pin it, the date moves with that set, and the suite's list of
  the major (`quantum/docs/planes/quantum-2.0.md` §3.0, row 34) carries the
  date that governs.
- The date opens the window; the version is where the change can land.
  Whichever comes second governs.

## Validation

- **The v1 behaviour is pinned** in `server/peer_allowlist_test.go`:
  `TestServer_PeerAllowlistOff_AcceptsAnyPeerAndWarnsOnce` (the default
  accepts an unconfigured peer, with one WARN per `server_id`) and
  `TestServer_PeerAllowlistOff_WarnsOnACertificateThatNamesAnotherServer`.
- **What the default becomes is exercised**:
  `TestServer_PeerAllowlist_RefusesAPeerItDoesNotDial`,
  `TestServer_PeerAllowlist_AcceptsTheServersItDials` (two servers that
  dial each other, with and without the flag, start together and accept
  each other) and
  `TestServer_PeerAllowlist_UnderMutualTLSTheCertificateNamesThePeer`.

  ```sh
  cd server && go test -count=1 -run 'TestServer_PeerAllowlist' .
  ```

  The 2.0 PR changes the two `PeerAllowlistOff` tests (the default then
  refuses) in the same commit as the default.
- **Release note:** the release that carries this file announces it.
- **Rollback plan:** not applicable in v1 — nothing changes by default.

## Timeline

- Found: `2026-10-23` (re-audit, OR-103).
- Listed for the major: `2026-10-27` (A12 `R1`, row 34 of the closed list).
- Written, with the opt-in: `2026-11-03` (A12 `M0-R`).
- Review checkpoint: `2027-02-07`, when the window opens.
- Change: at `v2.0.0` planning (`M1`), in one PR that applies the check on
  every server and changes the pinned tests.
