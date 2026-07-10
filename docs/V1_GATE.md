# v1.0 Gate — what an honest tag still requires

> **Date:** 2026-07-10 · **Current version:** v0.3.0
> **Origin:** Quantum suite Fase 5 ([QADR-0005](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0005-secuenciacion-convergencia.md)):
> orbit converges to v1.0 in lockstep, consuming nucleus v1.0.0 — the frozen,
> promised surface. Nucleus tagged its major 2026-07-10; the repin landed the
> same day.
> **Precedent:** nucleus's `docs/V1_GATE.md` (and Quark's before it) — a
> qualitative, verifiable checklist; v1.0 is NOT tagged until every §A item is
> closed or explicitly waived in §B with a commit that documents the decision.
> **Scale note:** orbit's public Go surface is deliberately small (the
> `Module(cfg)` entrypoint + the `datasource` contract + two opt-in bridges),
> so this gate is proportionally smaller than nucleus's — same discipline,
> fewer items.

## Why this document exists

Orbit's v1.0 makes two promises at once: the **`datasource` contract freezes**
(its ADR-001 explicitly defers the freeze to v1.0 — third-party data sources
can then implement it without fear), and the **module wiring surface**
(`orbit.Config`, `orbit.Module`) becomes as binding as the nucleus surfaces it
consumes. This gate lists everything that would make those promises dishonest
today.

## Current standing (verified 2026-07-10)

| Check | Status |
|---|---|
| Consumes nucleus v1.0.0 by tag (all modules) | ✅ repinned (orbit#16) |
| Six modules build + test standalone (`GOWORK=off`) | ✅ green |
| Suite lockstep lane (orbit tests vs pinned nucleus) | ✅ green on every quantum push/PR |
| `datasource` contract validated by 2 implementations | ✅ Nucleus adapter + quarkdatasource (ADR-001) |
| Honest-data policy in the admin UI (no simulated data) | ✅ since the redesign (orbit#15) |

---

## §A · Blocking items (close before v1.0)

### A-1 — `datasource` contract: declare final + add a freeze guard ✅ CLOSED 2026-07-11
ADR-001 defers the contract freeze to v1.0 — that moment is now. Two halves:

- **Declare the shape final**: `Record`, `Choice`, `FieldInfo`, `ForeignKey`,
  `Index`, `ModelInfo` (+`Field`), `ModelSource`, `Query`, `Page`,
  `CountResult`, `RecordStore`, `DataSource` — validated by two independent
  implementations (Nucleus adapter, quarkdatasource) with the three documented
  accommodations (composite-PK→read-only, Nullable absorption, alias ignored).
- **Add the machinery the promise needs**: orbit has no contract-freeze
  guard. A minimal exported-symbols baseline test (nucleus's pattern, scaled
  down) covering `orbit` (root) + `orbit/datasource` — so a v1.x PR that
  removes or renames a frozen symbol fails CI instead of relying on review
  memory.

**Closed when:** the baseline test exists and is green in CI, and ADR-001
carries the freeze note.

**Closure (slice 1):** `contracts/freeze_test.go` pins `orbit` (root) +
`orbit/datasource` against `contracts/baseline/api_exported_symbols.txt`
(100 symbols; both directions fail — removals AND unreviewed additions);
deliberate changes rebaseline via `ORBIT_UPDATE_CONTRACT_BASELINE=1`. The
suite's `orbit-lockstep` lane covers `./orbit/...`, so the guard runs on
every quantum push/PR (orbit has no PR CI of its own — verified locally
with `GOWORK=off go test ./contracts`). ADR-001 carries the freeze section.
Note for A-3: the 21 `Config` fields are already IN the baseline; what A-3
still owes is the field-by-field review + the godoc v1.0 promise.

### A-2 — Fleet leg resolves standalone (agent/proto tags)
`orbit/agent` and `orbit/proto` are NOT in release-please (packages today:
root, quarkbridge, quarkdatasource) and have no tags; `agent` and `server`
carry intra-repo `replace` directives. Consequence: the fleet leg of a
consumer app (e.g. the suite showcase's `fleet` build tag) resolves only
inside this repo or via the suite workspace — `go get` of the agent from a
real app does not work.

- Add `agent` and `proto` as release-please packages (component tags
  `agent/vX`, `proto/vX`, `release-as: 0.1.0` for the first cut — the same
  honest-versioning call made for the bridges).
- After the first tags: drop the `replace` directives in `agent` and
  `server`; verify `GOWORK=off` resolution from a scratch module.
- Decide `server`'s distribution story: it is a deployable, not a library —
  either it also gets tags (go-install-able) or its in-repo-only build is
  documented explicitly.

**Closed when:** a scratch module outside the repo can `go get` the agent and
build the fleet leg against tags only.

### A-3 — `orbit.Config` shape final ✅ CLOSED 2026-07-11
`Config` is the whole public wiring surface of the module entrypoint. Review
every field for v1.0 fitness (naming, zero-value behavior, the
`DataSource` injection point, auth-DB alias resolution), then declare the
shape final in the same freeze baseline as A-1. Any field that is not ready
to freeze must be dispositioned explicitly (rename now / document / remove).

**Closed when:** the Config fields are in the baseline and their godoc states
the v1.0 promise.

**Closure (slice 3):** all 21 fields reviewed field-by-field — every one is
fit to freeze as-is (no renames, no removals; the four zero-value defaults
are coherent and applied in `Module()`: Prefix→`/admin`,
MigrationsPath→`migrations`, AuditMaxSize→`10000`, ClusterChannel→
`nucleus:admin:live:v1`). The Config doc comment now states the v1.0 promise
explicitly; the `AuditMaxSize` default is spelled out (the sizing const is
unexported). The fields were already pinned in the freeze baseline by
slice 1.

### A-4 — Docs accuracy sweep vs the v1.0 surfaces ✅ CLOSED 2026-07-11
Orbit's docs live as the suite site instance (9 pages, written from READMEs
pre-v0.2). Sweep them against today's surfaces — Config fields, module
mounting, the datasource contract, the fleet leg — with the anti-falsehood
discipline (every symbol and key verified). The READMEs (root, agent, proto,
server) get the same pass.

**Closed when:** the sweep finds zero phantom symbols/keys and the pages
describe v1.0 behavior.

**Closure (slice 3):** full sweep of the 9 site pages, the 4 READMEs, both
fleet `doc.go` files, `go.work`, and CLAUDE.md, every cited symbol/key/flag
verified against source. Findings fixed: the bootstrap-password myth (docs
promised a generated random password on empty `bootstrap_password`; the code
SKIPS bootstrapping — 6 spots), the phantom agent wiring (`cfg.AdminAgent`,
`app.MustLoadConfig` — the real API is `agent.ExtensionConfig` +
`app.LoadConfig`), the phantom `--metrics-addr` server flag, the phantom
`RequireConnection` field and `BENCHMARKS.md` pointer, the false "CI verifies
regeneration" claim, `make build` documented as producing `bin/admin-server`
(it does not), stale v0.1.0 version claims, pre-extraction `admin/*` paths
and import lines, "Phase-1 skeleton" doc.go claims on implemented modules,
and the anti-hype `status: complete` badge.

---

## §B · Waiver candidates (explicit, or they don't count)

Proposed to the maintainer — each needs a documented decision:

1. **W1 — RBAC/audit RPCs for the Manage screens → v1.1.** The Access
   control and Audit log screens ship as UI with *declared gaps* (the
   honest-data policy from the redesign): no simulated data, an explicit
   "backend pending" state. Implementing the two RPC families before v1.0
   is real scope (M–L); the declared-gap posture is already honest.
   Additive to wire later.
2. **W2 — SQL row count in `SqlStatementEvent` → v1.1.** An additive proto
   field + emitter change; nothing in the frozen contract blocks adding it
   later.

---

## §C · Suggested slice plan (order matters)

| # | Slice | Size | Unblocks |
|---|---|---|---|
| 1 | ✅ Freeze guard (baseline test) + datasource contract declared final + ADR-001 note (A-1) | M | the core v1.0 promise |
| 2 | agent/proto into release-please + first tags + drop replaces + server distribution decision (A-2) | M | fleet leg standalone |
| 3 | ✅ `orbit.Config` review + docs sweep (A-3 + A-4) | S–M | wiring surface honest |
| 4 | Waiver decisions (§B) + Release-As 1.0.0 + RC via the suite lane → **tag v1.0.0** | S | Quantum 1.0 |

Each slice lands as its own PR; the suite's `orbit-lockstep` lane validates
every release candidate against the pinned trio before tagging (the A-7
procedure proven on nucleus's v0.11/v0.12/v1.0 tags).
