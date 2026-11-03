# Behaviour-Change Notice: `system_pulse` stops including the process environment at v2.0.0

- ID: `DEP-2026-004`
- Status: `active`
- Announced in: the next minor after `v1.20.1` (A12 `M0-R`; the change was
  listed for the major by the re-audit of 2026-10-23, row 33 of the suite's
  closed list)
- Behaviour flip: `v2.0.0`, no earlier than 2027-02-07 — major-only per the
  suite's deprecation policy; the suite groups every breaking change into the
  one major at the close of its A12 arc (QADR-0010). This is a **permission
  change**: nothing is removed, no `// Deprecated:` marker carries it.
- Scope: `behaviour` (permissions)
- Affected lifecycle tag: `stable` (the panel's HTTP API under the root
  module)
- Owner: `@jcsvwinston`

## Summary

The system snapshot (`GET <prefix>/api/system/snapshot`, behind the panel's
System and Overview screens) carries the process environment in its
`environment` field, and the snapshot is authorized by one action,
`system_pulse` on `admin:*`. Values whose name looks secret-bearing (`*URL*`,
`*TOKEN*`, `*PASS*`, `*KEY*`, `*AUTH*`, …) are masked and URL credentials
are redacted, but the rest is served in clear: on the host the re-audit
measured, 58 of 70 variables (`HOME`, `PATH`, `USER`, `TMPDIR` and, on
that host, an email address). An operator granted
`system_pulse` to watch goroutines and the connection pool reads all of it.

The environment moves to its own action, **`system_env`** on `admin:*`.

What changes and when:

- **This release:** `system_env` exists. A superuser, or an operator who
  holds `system_pulse` **and** `system_env`, reads the environment exactly as
  before. An operator who holds `system_pulse` without `system_env` still
  reads it — today's behaviour, unchanged — and the panel logs one WARN the
  first time that operator does so in the process, naming `DEP-2026-004`,
  the operator and the action to grant. The snapshot is polled by the
  Overview and System screens, so the WARN is once per operator per process,
  not per request. `system_env` widens `system_pulse` and opens nothing on
  its own: without `system_pulse` the snapshot answers `403`, as today.
- **v2.0.0:** an operator who holds `system_pulse` without `system_env` gets
  the snapshot with `"environment": []`. Everything else in the snapshot
  stays under `system_pulse`. The WARN goes away with the behaviour it
  announces.

## Affected surfaces

- The `environment` field of `GET <prefix>/api/system/snapshot` (and its
  `env_limit` query parameter, which only bounds that field).
- The `system_pulse` action on `admin:*`, which stops covering that field.
- The new `system_env` action on `admin:*`.
- Not affected: every other field of the snapshot (runtime, memory,
  databases, jobs, outbox, cluster, feature flags, telemetry);
  `GET <prefix>/api/system/flags`, which stays under `system_pulse`; the
  masking rules, which apply under either action; a superuser; the open
  posture (no auth provider), which has no operator to gate; a panel without
  an RBAC enforcer whose auth provider authorizes every action, as the
  built-in database provider does — its operators hold `system_env` already;
  and an operator whose policy grants every action (`*`) on `admin:*`, for
  the same reason. The fleet server and agent serve no environment.

## What changes at 2.0

| Operator | Today | At `v2.0.0` |
|---|---|---|
| superuser | environment | environment (unchanged) |
| `system_pulse` and `system_env` | environment | environment (unchanged) |
| `system_pulse` only | environment, one WARN per operator per process | **`"environment": []`**, no WARN |
| `system_env` only | `403` on the snapshot | `403` on the snapshot (unchanged) |
| neither | `403` on the snapshot | `403` on the snapshot (unchanged) |
| open posture (no auth provider) | environment | environment (unchanged) |

## Migration path

- **Now (v1):** decide who needs the environment and grant them
  `system_env` on `admin:*`. The operators who only watch the runtime need
  nothing; the WARN names each operator who reads the environment without
  it, so the log is the list to decide on.

  From the panel's Access Control screen, the policy
  `(ops, admin:*, system_env)` — the screen posts
  `{"sub":"ops","obj":"admin:*","act":"system_env"}` to
  `<prefix>/api/rbac/policies`. In Go, on the application's authorizer:

  ```go
  enf.AddPolicy("ops", "admin:*", "system_env")
  ```

  In a Casbin policy file: `p, ops, admin:*, system_env`. An application
  with its own `AdminAuth` provider and no enforcer answers
  `Authorize(user, "*", "system_env")`.
- **At 2.0:** nothing to do for an operator granted `system_env`. An
  operator without it sees an empty environment list.
- **Behaviour differences, from the operator's side:** at the major, an
  operator with `system_pulse` alone sees the snapshot without the
  environment. A client that reads `environment` gets an empty array, never
  `null` and never a missing field.
- **Required changes:** one policy row per subject that needs the
  environment, or none.

## Detection

- At runtime, the panel logs
  `orbit: the system snapshot serves the process environment to an operator without system_env; system_pulse stops including it at Orbit 2.0 (DEP-2026-004)`
  with `operator_id`, `operator`, `role` and a `hint` naming `system_env`,
  once per operator per process. A panel that never logs it is not affected.
- In the policies: the subjects granted `system_pulse` on `admin:*` without
  `system_env` (or `*`) are the ones the change reaches —
  `GET <prefix>/api/rbac/policies`, or
  `grep -E 'system_pulse|system_env' policy.csv`.

## What breaks if this is ignored

At `v2.0.0`, an operator granted `system_pulse` without `system_env` sees
the System screen without the environment, and a script that reads
`environment` from the snapshot under such an account gets an empty list.
Nothing else in the snapshot changes, and no request that succeeds today is
refused.

## Change window

- **Why `v2.0.0`:** removing data an operator receives today under a
  published action is a behaviour change, and a published behaviour does not
  change within `v1.x` (QADR-0010).
- **Why `2027-02-07`:** the suite's support policy sets a minimum of 90
  calendar days of notice, counted from the set that publishes the notice.
  This notice is dated 2026-11-03; the first set in cadence that can pin it
  is the one of 2026-11-09, and 90 days later is 2027-02-07. If a later set
  is the first to pin it, the date moves with that set, and the suite's list
  of the major (`quantum/docs/planes/quantum-2.0.md` §3.0, row 33) carries
  the date that governs.
- The date opens the window; the version is where the change can land.
  Whichever comes second governs.

## Validation

- **The v1 behaviour is pinned** in
  `internal/admin/system_env_permission_test.go`:
  `TestSystemSnapshot_SystemPulseOnly_ServesEnvironmentWithWarning_V1`
  (`system_pulse` alone serves the environment, with one WARN per operator
  across repeated snapshots and a separate one for a second operator), under
  an RBAC enforcer and under the provider path.
- **The way to stay is exercised**:
  `TestSystemSnapshot_SystemEnv_ServesEnvironment` (`system_pulse` and
  `system_env`: the environment, masked, and no WARN),
  `TestSystemSnapshot_Superuser_ServesEnvironmentWithoutWarning`,
  `TestSystemSnapshot_WithoutSystemPulse_Refused` (neither action, or
  `system_env` alone: `403`, no environment in the body) and
  `TestSystemSnapshot_OpenPosture_ServesEnvironment`.

  ```sh
  go test -count=1 -run 'TestSystemSnapshot_' ./internal/admin/
  ```

  The 2.0 PR changes
  `TestSystemSnapshot_SystemPulseOnly_ServesEnvironmentWithWarning_V1` (the
  same operator then gets `"environment": []` and no WARN) and removes
  `Panel.warnSystemEnvThroughPulse`, in the same commit as the behaviour.
- **Release note:** the release that carries this file announces it.
- **Rollback plan:** not applicable in v1 — nothing changes.

## Timeline

- Masking by name and URL redaction shipped: `v1.8.18` (orbit#380,
  2026-09-04), which closed half of the first audit's finding on the
  snapshot; the other half, the environment under its own action, is this
  notice.
- Listed for the major: `2026-10-27` (the re-audit of 2026-10-23, row 33 of
  the closed list).
- Written: `2026-11-03` (A12 `M0-R`).
- Review checkpoint: `2027-02-07`, when the window opens.
- Change: at `v2.0.0` planning (`M1`), in one PR that empties the
  environment for `system_pulse` alone and changes the pinned test.
