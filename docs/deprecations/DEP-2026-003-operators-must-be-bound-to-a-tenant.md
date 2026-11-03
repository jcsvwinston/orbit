# Behaviour-Change Notice: an operator with no tenant is refused in a multi-tenant application at v2.0.0

- ID: `DEP-2026-003`
- Status: `active`
- Announced in: the first release after `v1.20.1` (A12 `M0-R`; the flip is
  row 32 of the suite's list of the major, added by the re-audit of
  2026-10-23 as OR-80)
- Behaviour flip: `v2.0.0`, no earlier than 2027-02-07 — major-only per the
  suite's deprecation policy; the suite groups every breaking change into the
  one major at the close of its A12 arc (QADR-0010). This is a **change of
  what `multitenant_enabled` does to an operator account**: nothing is
  removed, no `// Deprecated:` marker carries it.
- Scope: `behaviour`
- Affected lifecycle tag: `stable` (`orbit.Config.MultiTenantEnabled` is a
  frozen v1 surface; its meaning for an unbound operator is what changes)
- Owner: `@jcsvwinston`

## Summary

With `multitenant_enabled` on, Data Studio is confined to the tenant the host
application resolves for each request. Until this release that was the only
input: the operator account had no tenant, so the panel confined an operator
to whatever tenant the request resolved. With Nucleus' `header` resolver the
tenant is the value of a header the client sends, and a plain operator of one
tenant — no superuser rights, no `tenant_switch` — who was refused
`?tenant=<other>` listed, exported, read by id and created rows in the other
tenant by naming it in the header (OR-80). The `tenant_switch` gate and its
`tenant.override` audit entry covered `?tenant=` only.

The operator account now carries a tenant (`tenant_id`, a nullable column of
`nucleus_admin_users`). An operator who is bound to one is confined to it by
the panel itself, whatever the request resolves. The owner listed the binding
for the major: in a multi-tenant application every operator who is not a
superuser and holds no `tenant_switch` is bound to a tenant, and one who is
not is refused rather than left to the resolver.

What changes and when:

- **This release:** the column, the `tenant_id` field of the operator API,
  and the confinement. A bound operator who may not switch tenants works in
  their tenant only: a request that resolves another tenant — from the header,
  the subdomain or `?tenant=` — is refused with `403`, reads and writes alike;
  a request that resolves no tenant gets theirs, ahead of
  `multitenant_default`. An operator with **no** tenant keeps today's
  behaviour — the resolver decides — and the panel logs one WARN per operator
  per process naming this notice and how to set the tenant. A superuser or a
  `tenant_switch` holder is not confined by the tenant on their account; the
  switching rules, and the audit of every switch, are unchanged.
- **v2.0.0:** in a multi-tenant application, a request from an operator with
  no tenant, who is not a superuser and holds no `tenant_switch`, is refused
  with `403` naming the missing tenant. A bound operator, a superuser and a
  `tenant_switch` holder behave as in this release.

Refused rather than silently confined: the host has already scoped the
request to the tenant it resolved (its database alias, its storage prefix),
and answering it with another tenant's rows would mix the two. The refusal is
the one `?tenant=` naming another tenant has answered since OR-23.

## Affected surfaces

- `orbit.Config.MultiTenantEnabled` (`multitenant_enabled`) — what it does to
  an operator with no tenant.
- The admin users table `nucleus_admin_users`: a nullable `tenant_id`
  column, added at mount to a table created before it (`NULL` for every
  existing operator, so the upgrade changes nobody's access).
- The operator API: `tenant_id` on `GET`/`POST /admin/api/admin-users` and
  `GET`/`PUT /admin/api/admin-users/{id}`. A `PUT` that does not name
  `tenant_id` keeps it (the panel's own operator screen sends email and
  superuser only); `"tenant_id": ""` clears it; `"all"` is refused, being
  `?tenant=all`.
- Every API and page request of the panel in a multi-tenant application:
  `tenantContextMiddleware` decides the request's tenant before Data Studio,
  exports, imports and fixtures read it.
- Not affected: single-tenant applications (the column is kept and ignored);
  superusers and `tenant_switch` holders; the open posture without an auth
  provider, which has no operator.

## What changes at 2.0

| Operator, `multitenant_enabled` on | Today | At `v2.0.0` |
|---|---|---|
| bound to a tenant, may not switch | confined to it; a request resolving another tenant is `403` | unchanged |
| **no tenant**, may not switch | follows the tenant each request resolves; one WARN per process | **`403`** on every request to the panel |
| superuser or `tenant_switch`, with or without a tenant | the resolver's tenant; `?tenant=` allowed and audited | unchanged |
| any, `multitenant_enabled` off | not confined | unchanged |

## Migration path

- **Now (v1):** give every operator who is not a superuser and holds no
  `tenant_switch` the tenant they work in. From the panel's API, as an
  operator with `operators_manage` on `admin:*`:

  ```http
  PUT /admin/api/admin-users/<id>
  Content-Type: application/json

  {"tenant_id": "acme"}
  ```

  or at creation, with `"tenant_id"` in the `POST /admin/api/admin-users`
  body. `nucleus createuser` has no tenant flag: set the tenant of an operator
  it created through the API afterwards, or in the table
  (`UPDATE nucleus_admin_users SET tenant_id = 'acme' WHERE username = 'ana'`).
  Write the tenant as the host resolves it: Nucleus lower-cases the tenant
  it reads from a header or a subdomain, and the panel compares tenants
  exactly. An operator who genuinely works across tenants gets
  `tenant_switch` on `admin:*` instead, and every switch they make is
  audited.
- **At 2.0:** an operator left without a tenant and without either right is
  refused by the panel; nothing else changes for them or anyone else.
- **Behaviour differences, from the operator's side:** once bound, an
  operator who relied on the header or the subdomain to reach another tenant
  gets `403` there. That is the defect this closes; the tenant they are bound
  to works as before, and a request that resolves no tenant now opens their
  tenant instead of being refused.
- **Required changes:** one value per affected operator account.

## Detection

- In the log, once per operator per process:
  `orbit: operator has no tenant in a multi-tenant application ...` with
  `operator`, `operator_id` and a `hint` naming the API call. An operator who
  never logs it is not affected.
- In the table, the candidates:

  ```sql
  SELECT id, username FROM nucleus_admin_users
   WHERE is_active = 1 AND is_superuser = 0
     AND (tenant_id IS NULL OR tenant_id = '');
  ```

  minus the ones holding `tenant_switch` (or `*`) on `admin:*`, directly or
  through a role (`GET /admin/api/rbac/policies`).

## What breaks if this is ignored

At `v2.0.0`, in an application with `multitenant_enabled` on, every operator
with no tenant who is not a superuser and holds no `tenant_switch` can no
longer use the panel: each request answers `403`, naming the missing tenant.
Operators who are bound, superusers and `tenant_switch` holders notice
nothing.

## Change window

- **Why `v2.0.0`:** a behaviour published in v1 — the resolver decides an
  unbound operator's tenant — does not become a refusal within `v1.x`
  (QADR-0010).
- **Why `2027-02-07`:** the suite's support policy sets a minimum of 90
  calendar days of notice, counted from the set that publishes the notice.
  This notice is dated 2026-11-03; the first set expected to pin it is the
  one of 2026-11-09, and 90 days later is 2027-02-07. If a later set is the
  first to pin it, the date moves with that set, and the suite's list of the
  major (`quantum/docs/planes/quantum-2.0.md` §3.0, row 32) carries the date
  that governs.
- The date opens the window; the version is where the change can land.
  Whichever comes second governs.

## Validation

- **The v1 behaviour is pinned** in `internal/admin/tenant_binding_test.go`:
  `TestTenantBinding_UnboundOperatorFollowsTheResolverAndWarns` — an operator
  with no tenant follows the header resolver to either tenant, the WARN is
  logged once over several requests and names this notice, and neither a
  superuser nor a single-tenant application is warned.
- **The binding is exercised**:
  `TestTenantBinding_BoundOperatorCannotReachAnotherTenantThroughTheResolver`
  (an operator bound to one tenant, without `tenant_switch`, naming the other
  in `X-Tenant-ID`: list, record, CSV export, export jobs, create and update
  refused with `403`, the other tenant's rows untouched; their own tenant
  works with or without the header),
  `TestTenantBinding_SwitchersAreNotConfinedByTheirAccountTenant`,
  `TestTenantBinding_OperatorMissingFromTheStoreIsRefused`, and, for the
  account, `TestOperators_TenantIsSetKeptAndCleared` and
  `TestEnsureAdminUsersTenantColumn_AddsItToAnOldTable` in
  `internal/admin/operators_test.go`. The bench control `CUST-07`
  (`internal/adminbench/probes_customization_test.go`, `probeMultiTenant`)
  measures the confinement end to end with Nucleus' header resolver and an
  operator bound to one tenant.

  ```sh
  go test -count=1 -run 'TestTenantBinding|TestOperators_Tenant|TestEnsureAdminUsersTenantColumn' ./internal/admin/
  go test -count=1 -run 'TestAdminBench/CUST-07' ./internal/adminbench/
  ```

  The 2.0 PR changes `TestTenantBinding_UnboundOperatorFollowsTheResolverAndWarns`
  (the unbound operator is then refused) in the same commit as the
  behaviour.
- **Release note:** the release that carries this file announces it.
- **Rollback plan:** not applicable in v1 — an operator with no tenant
  behaves as before, and clearing an operator's tenant
  (`"tenant_id": ""`) undoes the binding.

## Timeline

- Found: `2026-10-23` (re-audit of Quantum 1.42.0, OR-80; raised to P1 when
  the adversarial review showed the write).
- Listed for the major: `2026-10-27` (A12 `R1`, row 32).
- Binding shipped and notice written: `2026-11-03` (A12 `M0-R`).
- Review checkpoint: `2027-02-07`, when the window opens.
- Change: at `v2.0.0` planning (`M1`), in one PR that refuses the unbound
  operator and changes the pinned test.
