# Behaviour-Change Notice: the node identity is bound to the certificate by default at v2.0.0

- ID: `DEP-2026-001`
- Status: `completed`
- Announced in: the next minor after `v1.19.3` (A12 `M0-O`; the owner decided
  the flip on 2026-10-06 and listed it for the major on 2026-10-16)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per the
  suite's deprecation policy; the suite groups every breaking change into the
  one major at the close of its A12 arc (QADR-0010). This is a **default
  change of `server.Config`**: nothing is removed, no `// Deprecated:` marker
  carries it.
- Scope: `behaviour`
- Affected lifecycle tag: `stable` (the flag is what deployments run; the
  `v0.x` tag of the `server` module does not exempt it from the window)
- Owner: `@jcsvwinston`

## Summary

An agent listener that verifies client certificates (`--agent-client-ca`)
knows who holds the certificate; it does not, by itself, decide which node
that is. Today the server registers the `node_id` the agent declares and logs
a WARN when it differs from the certificate's Common Name — an agent holding
a certificate for `node-a` can sit in the fleet as `node-b` (OR-57, born in
the A9 arc). `server.Config.AgentIdentityFromCertificate`
(`--agent-identity-from-cert`) binds the two: a registration whose `node_id`
is not the verified Common Name is refused with `PermissionDenied`. It is
opt-in because a fleet that shares one certificate across its agents would
stop registering the day the default changed, and a published default does
not change within `v1.x`.

The owner decided that the binding is the default at the major, and that a
fleet which shares a certificate says so instead of relying on the default.

What changes and when:

- **This release:** `server.Config.AllowSharedAgentCertificate`
  (`--allow-shared-agent-certificate`, env
  `NUCLEUS_ADMIN_ALLOW_SHARED_AGENT_CERTIFICATE=1`) is added. It keeps
  registering the declared `node_id` when it is not the certificate's Common
  Name — today's regime — and keeps it across the major. It contradicts
  `AgentIdentityFromCertificate`, so `Run` refuses to start with both set. A
  listener that verifies client certificates with **neither** field set
  starts as before and logs one WARN at start naming the two fields, so the
  deployment chooses in its own time rather than at the upgrade. A mismatch
  is still logged as a WARN per registration, with both identities: tolerated,
  never silent.
- **v2.0.0:** a listener that verifies client certificates binds the node
  identity to the certificate unless `AllowSharedAgentCertificate` is set: a
  registration whose `node_id` is not the Common Name is refused with
  `PermissionDenied`, as `AgentIdentityFromCertificate` does today.
  `AgentIdentityFromCertificate` keeps its meaning (on a listener that does
  not verify certificates it still refuses to start); setting it is no longer
  necessary under mutual TLS. Nothing changes for a listener that does not
  verify client certificates: there is no certificate to bind to.

## Affected surfaces

- `server.Config.AgentIdentityFromCertificate` — its default under a
  listener with `AgentTLS.ClientAuth == tls.RequireAndVerifyClientCert`.
- The `admin-server` binary: `--agent-identity-from-cert` /
  `NUCLEUS_ADMIN_AGENT_IDENTITY_FROM_CERT` (the default under
  `--agent-client-ca`) and the new `--allow-shared-agent-certificate` /
  `NUCLEUS_ADMIN_ALLOW_SHARED_AGENT_CERTIFICATE`.
- The `AgentService.Stream` registration path of the server, which applies
  the binding.
- Not affected: a listener without client-certificate verification (token
  auth, or `--insecure-agent-listener`); the agent, which already registers
  as its certificate's Common Name when it comes from files and no `node_id`
  is set; the per-registration WARN, which stays in every regime that
  tolerates a mismatch.

## What changes at 2.0

| Deployment | Today | At `v2.0.0` |
|---|---|---|
| `--agent-client-ca`, neither flag | registers the declared `node_id`, WARN per mismatch, WARN at start naming the two flags | **refuses** a `node_id` that is not the Common Name (`PermissionDenied`) |
| `--agent-client-ca --agent-identity-from-cert` | refuses the mismatch | refuses the mismatch (unchanged) |
| `--agent-client-ca --allow-shared-agent-certificate` | registers the declared `node_id`, WARN per mismatch | registers the declared `node_id`, WARN per mismatch (unchanged) |
| both flags | refuses to start | refuses to start (unchanged) |
| no client-certificate verification | registers the declared `node_id` | registers the declared `node_id` (unchanged) |

## Migration path

- **Now (v1):** decide, per server, and write it down in its configuration.
  Either is quiet at start; the decision is what the WARN asks for.

  ```sh
  # one certificate per node, named after it — the recommended posture
  admin-server --agent-client-ca ca.pem --agent-cert s.pem --agent-key s.key \
    --agent-identity-from-cert

  # a fleet whose agents share one certificate: say so, and keep it at 2.0
  admin-server --agent-client-ca ca.pem --agent-cert s.pem --agent-key s.key \
    --allow-shared-agent-certificate
  ```

  In Go: `server.Config{AgentIdentityFromCertificate: true}` or
  `server.Config{AllowSharedAgentCertificate: true}`.
- **At 2.0:** a server with `--agent-client-ca` and neither flag binds. The
  agents it served must present a certificate whose Common Name is their
  `node_id` — an agent whose certificate comes from files and that sets no
  `node_id` already does.
- **Behaviour differences, from the operator's side:** at the major, an agent
  that registered under a name other than its certificate's drops out of the
  fleet with `PermissionDenied` on the stream, logged by the server with both
  names. Nothing else changes: the refusal is the one
  `--agent-identity-from-cert` applies today.
- **Required changes:** one flag, or one certificate per node.

## Detection

- At start, the server logs
  `agent listener verifies client certificates but does not bind node identity to them`
  with the hint naming `DEP-2026-001` and both flags: this server has not
  chosen.
- Per registration, `admin agent registered under a node_id that differs from its client certificate`
  with `node_id` and `certificate_cn`: this fleet relies on the regime that
  changes. A fleet that never logs it is not affected either way.
- In configuration: `grep -E 'agent-identity-from-cert|allow-shared-agent-certificate|AGENT_IDENTITY_FROM_CERT|ALLOW_SHARED_AGENT_CERTIFICATE'`
  over the unit files and environment files of each `admin-server`; a server
  with `--agent-client-ca` and no hit is the one to decide for.

## What breaks if this is ignored

At `v2.0.0`, on a server that verifies client certificates and sets neither
flag, every agent whose declared `node_id` is not its certificate's Common
Name is refused at registration and disappears from the fleet views; its
events, snapshots and Data Studio requests stop with it. The server says why,
once per attempt, with both names. A fleet whose certificates already name
their nodes notices nothing.

## Change window

- **Why `v2.0.0`:** a published default does not change within `v1.x`
  (QADR-0010); this one refuses registrations that are accepted today.
- **Why `2027-01-17`:** the suite's support policy sets a minimum of 90
  calendar days of notice, counted from the set that publishes the notice.
  The owner decided on 2026-10-06; this notice is dated 2026-10-18; the first
  set in cadence that can pin it is the one of 2026-10-19, and 90 days later
  is 2027-01-17. If a later set is the first to pin it, the date moves with
  that set, and the suite's list of the major
  (`quantum/docs/planes/quantum-2.0.md` §3.0, row 29) carries the date that
  governs.
- The date opens the window; the version is where the change can land.
  Whichever comes second governs.

## Validation

- **The v1 behaviour was pinned** in `server/node_identity_test.go`:
  `TestServer_AgentIdentityFromCertificate_Unbound_WarnsAndRegisters` (the
  default registered the declared `node_id`, with the WARN) and
  `TestServer_AgentIdentityFromCertificate_Bound` (what the default became).
  The fleet bench control `IDENT-06`
  (`internal/fleettest/fleetbench/probes_identity_test.go`,
  `probeNodeIdentityBoundToCert`) recorded the default as its second fact and
  the page `docs/fleet-bench.md` said it.
- **The 2.0 behaviour is pinned** (applied on `dry-run/2.0`):
  `TestServer_AgentIdentityFromCertificate_Bound` runs twice — `by_default`
  (neither field set) and `stated_by_the_field` — and
  `TestServer_AgentIdentityFromCertificate_Unbound_WarnsAndRegisters` is
  gone; its assertions live in
  `TestServer_AllowSharedAgentCertificate_KeepsTheDeclaredNodeID`, which also
  asserts the per-registration hint names `--allow-shared-agent-certificate`.
  `IDENT-06` records the default as its first fact (refused with
  `PermissionDenied`) and `AllowSharedAgentCertificate` as its second.
- **The exit is exercised**:
  `TestServer_AllowSharedAgentCertificate_KeepsTheDeclaredNodeID`,
  `TestServer_MutualTLS_BindingChoiceIsQuietAtStartup` (the 1.x name was
  `TestServer_MutualTLSWithoutABindingChoice_WarnsAtStartup`; since 2.0 it
  asserts silence at start in all three regimes, because the start-up WARN
  asked for a choice the default now makes) and
  `TestServer_AllowSharedAgentCertificate_ContradictsBinding_RefusesToStart`.

  ```sh
  cd server && go test -count=1 \
    -run 'TestServer_AgentIdentityFromCertificate|TestServer_AllowSharedAgentCertificate|TestServer_MutualTLS_BindingChoiceIsQuietAtStartup' .
  cd internal/fleettest && go test -count=1 -run 'TestFleetBench/IDENT-06' ./fleetbench/
  ```

  The 2.0 PR flipped the default, changed those tests, the second fact of
  the `IDENT-06` probe and the paragraph of `docs/fleet-bench.md` that
  explains it, in the same commit (`server/server.go`:
  `bindsNodeIdentityToCertificate()` =
  `AgentIdentityFromCertificate || (tlsRequiresClientCert(AgentTLS) && !AllowSharedAgentCertificate)`).
  Note for the train: `internal/fleettest/go.mod` pins `orbit/server` at
  `v0.19.0`; harmless under the workspace (CI runs that module through
  `go.work`), but a `GOWORK=off` run of the fleet bench would see the 1.x
  server and report `IDENT-06` partial until the align step bumps that pin
  to the 2.0 server release.
- **Release note:** the release that carries this file announces it.
- **Rollback plan:** not applicable in v1 — nothing changes.

## Timeline

- Binding shipped, opt-in: `server/v0.13.0` (A9, where OR-57 was born).
- Decided: `2026-10-06` (the owner, with the other changes of the major).
- Written: `2026-10-18` (A12 `M0-O`).
- Review checkpoint: `2027-01-17`, when the window opens.
- Change: at `v2.0.0` planning (`M1`), in one PR that flips the default
  under client-certificate verification and changes the pinned tests.
- Applied: `dry-run/2.0`, 2026-10-18 (the 2.0 dry run; the start-up WARN
  and `identityBindingUndecided()` are gone, the two `Run` refusals stay).
