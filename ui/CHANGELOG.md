# Changelog

## [1.1.0](https://github.com/jcsvwinston/orbit/compare/ui/v1.0.0...ui/v1.1.0) (2026-10-05)


### Added

* **admin:** actions on one record, and actions that answer with a redirect inside the panel or a confined download (A11 O4) ([#535](https://github.com/jcsvwinston/orbit/issues/535)) ([5ba6c53](https://github.com/jcsvwinston/orbit/commit/5ba6c5378795784044bbad2ccec649911711983f))
* **admin:** actions that ask before they run — declared fields, validated on the server, rendered as a form (A11 O3) ([#533](https://github.com/jcsvwinston/orbit/issues/533)) ([12c234c](https://github.com/jcsvwinston/orbit/commit/12c234c48612d6a662d6b00097480c53ec003790))
* **admin:** built-in widgets fed by Go callbacks, and more than one dashboard, each behind its own permission (A11 O5) ([#538](https://github.com/jcsvwinston/orbit/issues/538)) ([e64c60a](https://github.com/jcsvwinston/orbit/commit/e64c60aa790a0dbff8a550b1d3478d45034e249e))
* **admin:** the application's own client code under the panel's CSP, and field renderers it registers (A11 O6) ([#540](https://github.com/jcsvwinston/orbit/issues/540)) ([5fba1fb](https://github.com/jcsvwinston/orbit/commit/5fba1fbf2a5f0024442d15488cb1da9b38422348))
* **admin:** the panel keeps the promises its configuration makes — field widgets checked at startup, branding that loads under the CSP, the logo on the login screen, and every key in the reference (A11 O1) ([#532](https://github.com/jcsvwinston/orbit/issues/532)) ([b072e98](https://github.com/jcsvwinston/orbit/commit/b072e988590072b035376ea1f6f9d87d43bd9d46))
* **admin:** the theme comes from the configuration — applied before the first frame, the operator's choice still wins, and a palette checked per theme (A11 O2) ([#534](https://github.com/jcsvwinston/orbit/issues/534)) ([2bcfe55](https://github.com/jcsvwinston/orbit/commit/2bcfe5530693088babd629257c98de8a744b3f93))

## 1.0.0 (2026-09-25)


### Added

* **fleet:** ADR-012, the wire declares operator identity and filters, and lists carry an exact total (A9 S3, part 1) ([#506](https://github.com/jcsvwinston/orbit/issues/506)) ([d542622](https://github.com/jcsvwinston/orbit/commit/d542622a12df088c62467d8d4e5794746726d7d9))
* **proto:** the audit entry says what changed, and the agent can return the previous values (A9 S5, part 1) ([#512](https://github.com/jcsvwinston/orbit/issues/512)) ([29200ff](https://github.com/jcsvwinston/orbit/commit/29200fffcf794770e747567908933cdaba9080a0))
* **proto:** the wire declares what retention, alerts and a fleet of servers need — one additive change for A9 S6 to S8 ([#518](https://github.com/jcsvwinston/orbit/issues/518)) ([307d4ed](https://github.com/jcsvwinston/orbit/commit/307d4ed3550a92e73518d5fb25f6c579e424b6ea))
* **ui:** cierra los 3 restos de UI del backlog v1.2.1 — i18n centralizado, a11y de tablas y consolidación de tablas del panel ([#123](https://github.com/jcsvwinston/orbit/issues/123)) ([6fc0332](https://github.com/jcsvwinston/orbit/commit/6fc0332b2d6076a1746ee4a052e7335592d35b11))
* **ui:** connect-es 2 from proto, the browser instrument over the fleet, and the tenant in the UI (A9 S10) ([#526](https://github.com/jcsvwinston/orbit/issues/526)) ([8b49256](https://github.com/jcsvwinston/orbit/commit/8b492562d44facf34e457757f1ec6fe0270a7b3a))
* **ui:** one frontend project — the panel as the base, the fleet as its second entry, one dist embedded by a ui module both binaries require (A9 S9) ([#524](https://github.com/jcsvwinston/orbit/issues/524)) ([d596ca1](https://github.com/jcsvwinston/orbit/commit/d596ca16a9bec5f6730179d6dce733d8df8f9b23))
