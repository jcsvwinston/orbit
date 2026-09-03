---
name: Bug report
about: Something in Orbit does not behave as documented
title: ""
labels: bug
assignees: ""
---

<!--
Security issues: do NOT open a public issue. Follow SECURITY.md instead.
-->

## What happened

A clear description of the behaviour you observed.

## What you expected

What the documentation (website/docs, README, godoc) says should happen.

## Steps to reproduce

1. Configuration (relevant `orbit.Config` fields or `admin-server` flags — redact tokens, DSNs and certificates):
2. Request / action:
3. Observed result (status code, log line, screenshot):

## Versions

- Orbit root module (`go list -m github.com/jcsvwinston/orbit`):
- Fleet modules, if involved (`agent`, `server`, `proto` tags):
- Nucleus version (`go list -m github.com/jcsvwinston/nucleus`):
- Database engine and version:
- Go version (`go version`) and OS:

## Which surface

- [ ] In-process panel (`orbit.Module`, Data Studio, live feed, RBAC, audit)
- [ ] Fleet plane (`agent/`, `server/`, `admin-server` binary)
- [ ] Quark integration (`quarkbridge`, `quarkdatasource`)
- [ ] Documentation

## Logs

Relevant log lines (structured `slog` output), redacted.
