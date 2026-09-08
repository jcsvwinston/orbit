# Security Policy

Orbit is an **admin panel**: it mounts a privileged surface (data browsing
and editing, session management, RBAC administration) inside your
application's process. Vulnerabilities in it are correspondingly serious,
and reports are handled with priority.

## Supported versions

Security fixes land on `main` and are released from it. Fixes are applied
to the **latest tagged minor** of each module; older tags are not patched —
upgrade to the current tag to receive security updates. The current
released versions are listed in the repository's release page and in the
module compatibility matrix under `website/docs/reference/`.

## Reporting a vulnerability

**Please do NOT open a public GitHub issue for security vulnerabilities.**

Report privately using one of:

1. **GitHub private security advisory (preferred):**
   [Security → Report a vulnerability](https://github.com/jcsvwinston/orbit/security/advisories/new)
   in this repository.

2. **E-mail:** serrano.juan.carlos@gmail.com

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce or a proof-of-concept.
- Affected module(s) (`orbit`, `agent`, `server`, `proto`, `quarkbridge`,
  `quarkdatasource`) and version(s).
- Any suggested remediation, if known.

You will receive an acknowledgement within **72 hours** and a substantive
response within **7 days**.

## What is in scope

Anything reachable through Orbit's surfaces:

- The **in-process panel** (`/admin` by default): authentication and session
  handling, RBAC checks (`authorizeAction`), tenant isolation in Data
  Studio, the audit log, import/export, the storage browser's path
  confinement.
- The **fleet plane** (`agent/` + `server/`): the agent listener's
  fail-closed startup contract, agent/server authentication (shared token,
  TLS and mutual TLS at the agent listener),
  the UI listener's trust boundaries, and the server-side gates on fleet
  Data Studio mutations.
- **Bypass of a documented default**: the panel registers under the
  framework's default-deny authorization and gates every request below its
  prefix behind its own login; the delegated authentication chain never
  bypasses the panel's own admin table. If you can get past any of these
  without the documented opt-outs, that is a vulnerability even if no data
  is exfiltrated.

Dependencies are monitored with `govulncheck` in CI on every PR; the source
of truth for toolchain advisories is the
[Go vulnerability database](https://vuln.go.dev/). Orbit does not maintain
its own advisory list.

## Static analysis of this code

`.github/workflows/codeql.yml` runs CodeQL over Orbit's Go on every pull
request and once a week on `main`. `govulncheck` already reads our
dependencies; this reads what we wrote. It follows a value from where it
enters the program to where it is used, which is the only way to see a
request parameter reaching a query, a path or a command several calls away —
exactly the shape of the surfaces listed above.

What it covers is what the workflow builds: the root module, `agent`,
`server`, `quarkbridge` and `quarkdatasource`. `proto` is generated code and
`internal/fleettest` is a test-only module, so neither is built and neither
is analysed. For a compiled language CodeQL extracts what the build compiles,
which is why the exclusion is a module missing from that build and not a
`paths-ignore` glob (those do not apply to Go).

The lane reports and does not gate: no check fails because of an alert.

## Verifying what you downloaded

Each root release (`vX.Y.Z`) publishes the `admin-server` binary with an SPDX
bill of materials per archive, a checksum file, a keyless cosign signature
over that checksum file and a GitHub build provenance attestation. Every
other module is a library and ships no executable of its own; those arrive
through the Go module proxy, where the checksum database is the equivalent
guarantee. The runnable examples in the tree (`examples/minimal`,
`agent/examples/fleet-app`) are demos, not release artefacts, and are
published nowhere.

Check a downloaded binary before you run it — the commands are in
`website/docs/operations/verifying-releases.md`. An asset that verifies
against an identity other than
`https://github.com/jcsvwinston/orbit/.github/workflows/release.yml@refs/tags/<tag>`
is a report worth making through the channels above.

## Disclosure policy

We follow a **90-day coordinated disclosure** timeline:

1. Vulnerability reported privately.
2. Acknowledgement and investigation begin (≤ 72 h).
3. A fix is developed on a private branch.
4. A patched release is published.
5. A GitHub security advisory is published (with the release, or up to
   7 days later).

Reporters are credited in the advisory unless anonymity is requested.
