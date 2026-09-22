# Fleet bench — what the fleet plane can and cannot do today

This is the numerator of the A9 gate ("one fleet, one web UI"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-09-20 against the checkout at this commit — the agent,
server and protocol modules identical to their tagged releases in the
v1.10.3 set — and kept current as the arc closes its gaps: the numbers below
are what the suite produced on its last run.** Run it with:

```bash
cd internal/fleettest
go test ./fleetbench/ -run TestFleetBench -v
go test ./fleetbench/ -run TestFleetBenchSummary -v                   # per-family counts
ORBIT_FLEET_BENCH_TABLE=1 go test ./fleetbench/ -run TestFleetBenchTable   # writes fleetbench/bench-table.md
```

The last command writes `internal/fleettest/fleetbench/bench-table.md`, a
generated file that is not committed; the tables under "The result" are
pasted from it by hand when a verdict moves, so the page and the catalogue
say the same thing.

The bench is not prose. Every control is a Go probe in
`internal/fleettest/fleetbench/` that boots a real admin server and a real
agent with the configuration the question needs — TLS or not, a token or a
client certificate, an allowlist or none, one server or two — and asks the
fleet's own surface the question an operator would ask: the Connect services
on the UI listener, the agent listener, the metrics listener, the server's
registry. The controls about the two web UIs and about the protocol are
static: they read files and descriptors in the checked-out tree — which dist
each binary embeds, which job diffs it, which generators emit the stubs,
which fields a message declares. None of them is decided by the absence of a
word alone. `TestFleetBench` asserts the **recorded verdict** rather than
success, so closing a gap turns the suite red with "this one is present now,
update the verdict" — which is what keeps this page honest.

There was already a description of this plane: the reconnaissance that opened
the arc read the code and wrote down what it thought was there. Reading is a
hypothesis. Two of its hypotheses were wrong (below), and nothing but a probe
would have said so.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence: a stream accepted under the wrong name, an empty replay after a restart, a field the descriptor never declares |

A control that cannot be probed does not belong in the bench. A control whose
surface appears (a new field, a new configuration knob) turns its probe red
against the recorded verdict, and the probe then grows the behaviour check the
new surface makes possible.

A declaration is not a surface. `FDS-09` is **absent** although the protocol
declares `total` and `total_estimated` on every page: the server never
produces a number in them, filtered or not, so what the wire declares is a
place for a capability, not the capability.

## The result

**26 of 50 controls present. 3 partial. 21 absent.**

| family | present | partial | absent |
|---|---|---|---|
| identity | 10 | 0 | 0 |
| datasource | 10 | 1 | 1 |
| retention | 1 | 0 | 6 |
| alerts | 2 | 0 | 4 |
| ha | 1 | 1 | 3 |
| ui | 2 | 1 | 7 |
| **total** | **26** | **3** | **21** |

### alerts — 2 present · 0 partial · 4 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `ALR-01` | a threshold rule on a host metric raises an alert | **absent** | server.Config has no rule or threshold field and admin.proto declares no rule, threshold or alert message: the server stores the last HostMetrics sample per node and evaluates nothing against it. |
| `ALR-02` | alert channels (webhook, e-mail) exist | **absent** | server.Config has no webhook, SMTP or notification field and the protocol has no channel or recipient message: nothing on the server can be told where to send anything. |
| `ALR-03` | the UI API exposes alert state | **absent** | ControlService and ManageService (admin.proto) have no RPC and no message about alerts or incidents; the fleet UI has nothing to show. |
| `ALR-04` | the server publishes its own Prometheus collectors (nodes connected, events dropped) on the metrics listener | **absent** | the metrics listener (server.Config.MetricsAddr) serves the default registry only — go_* and process_* families; server.go mounts promhttp.Handler() and registers no collector of its own, which server/config.go calls future work. |
| `ALR-05` | the agent publishes its own Prometheus collectors on its metrics listener | **present** | — |
| `ALR-06` | a node that stops sending frames is listed as not connected within the inactivity timeout plus one janitor tick | **present** | — |

### datasource — 10 present · 1 partial · 1 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `FDS-01` | the fleet lists the models an agent registered | **present** | — |
| `FDS-02` | a record survives create, read, update and delete through the fleet once its model is allowlisted | **present** | — |
| `FDS-03` | mutations are refused by default; the allowlist opens a model; reads are never gated | **present** | — |
| `FDS-04` | a viewer operator (read-only role) can read and cannot mutate | **present** | — |
| `FDS-05` | the operator identity crosses the stream: the agent-side handler is told who is asking | **present** | — |
| `FDS-06` | the application's per-model policy applies to the fleet operator: a denied model is refused | **present** | — |
| `FDS-07` | fleet reads are tenant-filtered when the model declares a tenant column | **present** | — |
| `FDS-08` | filters with operators (contains, range, set, null) reach the agent | **present** | — |
| `FDS-09` | pagination carries an exact total, filtered or not | **present** | — |
| `FDS-10` | the agent serves Data Studio through the datasource contract, so a contract implementation can be registered in the fleet | **present** | — |
| `FDS-11` | a fleet mutation leaves an audit entry with operator, model, record and node, and says what changed | **partial** | ListAudit returns the entry attributed to actor, action, target (model and record id) and node; AuditEntry declares before_json and after_json (additive, with DataStudioResponse.previous for the agent to send the old values) and the server writes nothing into them yet: it uses them once it pins the proto tag that carries them. Whether the entry survives the process is RET-04's measurement. |
| `FDS-12` | the fleet-consumes-the-contract decision (docs/adrs/ADR-002) is recorded as implemented in the ADR and in the index | **absent** | the ADR's front matter says status: accepted and the index row in docs/adrs/README.md says "pendiente de implementar": the decision is taken and the work is open. |

`FDS-05`, `FDS-06`, `FDS-07` and `FDS-10` are **present** since A9 `S4`: the
agent serves Data Studio through the same `datasource` contract the panel
speaks (its own module since ADR-012, with the Nucleus adapter inside), and
the admin server sends the operator the UI auth chain resolved
(`DataStudioRequest.operator`: subject, role, read-only, tenant). Under that
identity the agent puts the framework claims on the context (a model hook
sees who is asking), applies the application's policy per model and verb
through its Authorizer (the panel's verbs: `list`, `retrieve`, `create`,
`update`, `delete`, `bulk_delete`), and confines a tenant-scoped operator to
its tenant — an equality filter on the model's tenant column for reads, the
tenant stamped on a create, ownership confirmed before an update or delete.
The tenant reaches the server through the trusted proxy's `X-Auth-Tenant`
header (`--ui-tenant-header`). A request without an operator — an older
server — behaves as before, with no identity, no policy and no tenant.
`UI-09` stays partial: the wire carries a tenant, the SPA still neither
sends nor shows one. `FDS-08` and `FDS-09` are **present** too: operator
filters are applied through the contract (an unknown operator is refused,
never dropped) and every list carries an exact total.

`FDS-11` stays partial after A9 `S5` (part 1) for the reason `FDS-05` and
`FDS-07` stayed partial after `S3`: the wire now declares what changed
(`AuditEntry.before_json`, `AuditEntry.after_json`, and
`DataStudioResponse.previous` for the agent to return the old values), and
declaring is not doing. The server and the agent fill them in part 2, once
they can pin the proto tag that carries the fields.

### ha — 1 present · 1 partial · 3 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `HA-01` | an agent fails over to the next endpoint when the first is unreachable | **present** | — |
| `HA-02` | two servers share the node registry: an agent connected to A is listed by B | **absent** | each server keeps its own in-memory node registry (server/nodes/registry.go); server B lists nothing about an agent connected to A. server/doc.go states active-active is not implemented. |
| `HA-03` | a UI subscribed on server B receives events from an agent connected to A | **absent** | events fan out inside the server that received them (server/routing/eventbus.go) and the server never talks to another server: a subscriber on B sees nothing an agent sends to A. |
| `HA-04` | agents are assigned across servers deterministically | **absent** | no shard, peer, cluster or assignment field in server.Config or agent.Config and nothing about it on the wire: an agent connects to the first endpoint in its list that answers /healthz. |
| `HA-05` | a reconnect under the same node_id supersedes the previous stream: one node, no duplicates | **partial** | the registry keeps one entry: Registry.Add (server/nodes/registry.go) evicts the old entry and cancels its context, which stops the server's writer. The old stream itself is not ended: AgentService.Stream's reader loop (server/services/agent_service.go:106-117) blocks in stream.Receive() and only checks streamCtx.Err() after Receive returns an error, so the superseded peer sees no error and a frame it sends after the takeover is still published as the node — a UI subscriber receives it. The old stream lives until its peer closes it. |

### identity — 10 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `IDENT-01` | the agent listener serves TLS: a cleartext client is refused, a TLS client negotiates h2 and is served | **present** | — |
| `IDENT-02` | with mutual TLS configured, a client without a certificate is refused at the handshake and a CA-signed one registers | **present** | — |
| `IDENT-03` | an agent listener off loopback refuses to start with neither a token nor verified client certificates | **present** | — |
| `IDENT-04` | a real agent connects over mutual TLS and registers; without a client certificate it never does | **present** | — |
| `IDENT-05` | the agent's client certificate can be given by configuration (file paths) rather than as a Go value | **present** | — |
| `IDENT-06` | the node identity is bound to the certificate: a certificate for node-a declaring node_id node-b is refused or registered as node-a | **present** | — |
| `IDENT-07` | the server's certificate rotates without a restart: a new handshake sees the new certificate | **present** | — |
| `IDENT-08` | the agent presents a new client certificate on its next connection without a restart | **present** | — |
| `IDENT-09` | a shared token authenticates an agent; a wrong token is refused with one rate-limited WARN naming the remote IP | **present** | — |
| `IDENT-10` | /healthz answers without credentials on both listeners while the same listeners refuse an uncredentialled RPC | **present** | — |

`IDENT-06` is **present** through an opt-in: `server.Config.AgentIdentityFromCertificate`
(`--agent-identity-from-cert`) refuses a registration whose `node_id` is not the
verified certificate's Common Name, and `Run` refuses to start with it on a
listener that does not verify client certificates. The default still registers
the declared `node_id` — with a WARN naming both — because deployments whose
certificate names something other than the node (one certificate shared by a
fleet) must keep working until the next major flips the default. The probe
measures both regimes. `IDENT-05` boots a real agent through the extension from
nothing but three file paths and no `node_id`; the node the server lists is the
certificate's Common Name.

`IDENT-07` and `IDENT-08` are **present** because both sides serve their
certificate *from* the files rather than copying it out of them once:
`server.TLSFromFiles` (what `--agent-cert`/`--agent-key` and `--ui-cert`/`--ui-key`
build) and the agent's `tls_cert_file`/`tls_key_file` re-read the pair when a
handshake finds the files changed, so a rotation is a write to two files and no
restart. The probes rewrite the files and handshake again: the server presents
the new certificate on the next connection, the agent on its next connection
(measured across a failover). A half-written rotation keeps the previous
certificate serving, with one WARN. The CA bundles (`--agent-client-ca`,
`tls_ca_file`) are still read once; rotating a CA is not a control yet.

### retention — 1 present · 0 partial · 6 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `RET-01` | events the server replays to a new UI subscriber survive a server restart | **absent** | the replay is a per-kind in-memory ring (server/routing/replay.go): a second server started on the same address replays nothing to a new subscriber, and server.Config has no persistence knob to change that. |
| `RET-02` | an event emitted while the agent has no stream is delivered after it reconnects | **absent** | the agent subscribes to the bus only while a stream is open and its ring buffer absorbs backpressure on an OPEN stream (agent/buffer/buffer.go): three events emitted during a server outage never arrive after the reconnect. |
| `RET-03` | host metrics have a history per node, not only the last sample | **absent** | the node registry keeps the latest HostMetrics per node (server/nodes/registry.go SetHostMetrics); NodeInfo carries one host_metrics message, not a series, and no RPC returns a history. |
| `RET-04` | the fleet audit trail survives a server restart | **absent** | the fleet audit is an in-memory ring of 2048 entries (server/routing/audit.go AuditRing): after a restart ListAudit is empty. The panel's own trail is a table with a retention window; the fleet's is not. |
| `RET-05` | a retention window is configurable and enforced | **absent** | server.Config has no retention, TTL, persistence or data-directory field: there is nothing to retain for and nothing to enforce it on. |
| `RET-06` | the fleet audit trail can be exported (CSV or JSON download) | **absent** | no route on the UI listener answers an export path (the single-page fallback catches them) and ManageService has only GetRbac and ListAudit; the panel's CSV export has no fleet counterpart. |
| `RET-07` | the replay buffer is bounded, drops the oldest and exposes its size and counters | **present** | — |

### ui — 2 present · 1 partial · 7 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `UI-01` | one frontend project serves both planes: the server and the panel embed the same dist | **absent** | two projects: ui/package.json builds into server/ui/dist (embedded by server/ui/embed.go) and internal/admin/ui/package.json builds into internal/admin/ui/dist (embedded by internal/admin/ui_fallback.go). |
| `UI-02` | the two planes share design tokens: one token source imported by both, or one project | **absent** | ui/src/index.css declares numbered --t0…--t53 custom properties and internal/admin/ui/src/index.css declares HSL shadcn-style ones; neither stylesheet nor tailwind config imports a file the other one does, relative or through a package that resolves inside the repository. |
| `UI-03` | the fleet UI has automated tests: a runner, a test script and at least one spec | **absent** | ui/package.json has no test script and no test runner among its devDependencies, and no *.test.* or *.spec.* file exists under ui/; the panel has vitest and a test suite. |
| `UI-04` | CI checks that the fleet UI's committed dist is fresh, as the panel's lane does | **absent** | the ui job in .github/workflows/ci.yml runs typecheck, lint and build in ui/ and never diffs server/ui/dist afterwards; the admin-ui job does exactly that for internal/admin/ui/dist. |
| `UI-05` | a bundle-size budget covers the fleet UI (a test constant, a size-limit configuration or a CI step) | **absent** | server/ui has no test file naming a byte budget (a constant like `<name>Budget = N * 1024`), ui/package.json has no size-limit configuration or tooling, and no CI job in ui/ enforces a size; the only budget in the repository is the panel's (internal/admin/ui_embed_test.go). |
| `UI-06` | the fleet UI's generated stubs are connect-es 2 / protobuf-es 2, in the dependencies and in the generators | **absent** | ui/package.json pins @connectrpc/connect ^1.6.1, @connectrpc/connect-web ^1.7.0 and @bufbuild/protobuf ^1.10.0, and proto/buf.gen.yaml pins the generators bufbuild/es:v1.10.0 and connectrpc/es:v1.6.1. |
| `UI-07` | the browser instrument covers the fleet UI: a Playwright spec navigates to a path outside /admin | **absent** | the only Playwright spec (internal/adminbench/browser/specs/panel.spec.ts) navigates to /admin paths only; nothing opens the fleet UI in a browser. |
| `UI-08` | the fleet UI is told the operator's role: GetSelf says read-only for a viewer | **present** | — |
| `UI-09` | the fleet UI has a tenant notion: a message on the wire carries one and the SPA sends or shows it | **partial** | the wire carries one since A9 S3 (OperatorIdentity.tenant on DataStudioRequest, server to agent), but nothing the fleet SPA sends (ui/src, outside src/gen) names a tenant and no screen shows which tenant an operator or a row belongs to: the Control surface still has no tenant field at all. S10 puts the tenant in the UI. |
| `UI-10` | the panel's initial load stays within its budget, and the budget is a test constant | **present** | — |

## What the shape of it says

The fleet plane **moves bytes safely and keeps nothing**.

- What is present is the transport and the gates. The agent listener serves
  TLS and mutual TLS, refuses to start exposed without authentication,
  accepts a shared token and turns a wrong one into a single, rate-limited
  warning that names the caller. A real agent registers over mutual TLS,
  fails over to the next endpoint, and publishes its own collectors. The
  fleet's data screen lists models and writes records; mutations are refused
  unless a model is allowlisted, a viewer cannot write, the UI is told who is
  read-only, and the replay ring is bounded and readable. That is a working
  fleet, and it is measured here so that no change can take it away in
  silence.
- **Identity is authenticated but not bound.** The server verifies the
  agent's certificate and then trusts the name the agent declares: a
  certificate for `node-a` registers as `node-b` if the agent says so
  (`IDENT-06`). The certificate's subject does reach the request, and stops
  there. Neither side can be told where its certificate files live, and
  rotation exists only as the Go-level hook a caller has to wire by hand
  (`IDENT-05`, `IDENT-07`, `IDENT-08`).
- **The fleet does not speak the data contract.** The panel's Data Studio
  and the fleet's are two implementations. On the wire, a Data Studio request
  carries no operator, no tenant, filters as equality pairs and no exact
  total; on the agent, the application's own policy is never consulted
  (`FDS-05` to `FDS-10`). The audit entry a fleet mutation leaves has the
  operator and the record but not what changed (`FDS-11`). The decision to
  make the fleet consume the contract is taken and recorded as pending
  (`FDS-12`).
- **Nothing persists.** A server restarted on the same address replays no
  events and remembers no audit (`RET-01`, `RET-04`); an event emitted while
  the agent had no stream is gone (`RET-02`); the only metrics sample per
  node is the last one (`RET-03`); there is no retention window because
  there is nothing to retain (`RET-05`), and nothing to export (`RET-06`).
- **Nobody is told.** No rule, no channel, no alert state, and the server's
  own metrics listener publishes the Go runtime's collectors and none of its
  own (`ALR-01` to `ALR-04`). What exists is the input side: the agent's
  collectors and a node marked stale when it goes quiet.
- **One server.** Two servers know nothing of each other's nodes or events,
  and nothing decides which agent goes where (`HA-02` to `HA-04`). The agent
  is ready for more than one; the server is not. And one thing the server
  does about its own nodes is half done: when an agent reconnects under a
  name that is still registered, the registry keeps one entry, but the old
  stream is never ended — its peer sees no error, and frames it still sends
  are accepted as the node (`HA-05`).
- **Two web UIs.** Two projects, two dists, two token systems, and one of
  each gate — tests, dist freshness, bundle budget, browser instrument — all
  on the panel's side (`UI-01` to `UI-07`).

## What the bench found that the reading did not

- **The fleet pager never has a total.** The reconnaissance expected the
  unfiltered page to carry an exact count and only the filtered one to answer
  "unknown". Measured, both answer `-1` / estimated: the agent never asks the
  model layer to count (`FDS-09` is absent, not partial). For context, not as
  a measurement of this bench: the panel's pager had the same gap and closed
  it in its own arc.
- **A superseded stream is not ended.** The reconnaissance read the
  registry — one entry per node, the old one evicted and its context
  cancelled — and expected `HA-05` present. Measured against the stream
  rather than the map: the cancellation stops the server's writer, but the
  reader loop is blocked waiting for the old peer's next frame and only
  looks at the cancelled context after that frame arrives, so the superseded
  peer is never told and keeps a live stream until it hangs up on its own
  (`HA-05` is partial). That is a defect in the supersession path, found by
  a probe that plays the old peer instead of counting entries.
- **An in-process server stop is not a restart.** The server ends its run
  with a graceful shutdown that waits for connections to go idle, and an open
  agent stream never does — so a stopped server keeps serving its agent from
  the same process, and an agent "connected" to it notices nothing. The first
  run of every restart control failed for that reason. The bench now puts a
  small TCP relay between agent and server and severs the connections when
  the server stops, which is what a process that died looks like from the
  agent. Worth knowing for anyone who tests a restart in-process: without the
  relay, three controls would have measured the harness.

## What this bench does not measure, and why

- **The fleet UI in a browser.** Contrast, focus order and keyboard reach do
  not exist until a browser has laid the page out; the panel has an instrument
  for that and the fleet UI does not (`UI-07`). When it does, its verdicts
  belong beside these, recorded the same way.
- **Engines other than SQLite.** The probes open SQLite in memory so the
  suite runs anywhere in seconds. They honour `ORBIT_TEST_DATASTUDIO_URL`, so
  a developer can point them at PostgreSQL or MySQL by hand; no CI lane does
  that for the bench today (the engine lane runs the fleet integration tests,
  not this package).
- **What a control is worth.** Seventeen present controls are seventeen whose
  probe exercised them, not seventeen that are good. What the number is for
  is that the next change to the fleet plane cannot quietly take one of them
  away, and that the arc's gaps are a list with a probe each, not a paragraph.
