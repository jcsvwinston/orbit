# Changelog

## [1.1.1](https://github.com/jcsvwinston/orbit/compare/ui/v1.1.0...ui/v1.1.1) (2026-10-07)


### Fixed

* **admin:** a field the operator may not read is refused as a filter, a sort or a search, and no longer labels a lookup, lists a saved view or orders a records card (OR-69) ([#550](https://github.com/jcsvwinston/orbit/issues/550)) ([95f16ba](https://github.com/jcsvwinston/orbit/commit/95f16badcd2383e4bb43c027fcfc3c1dff27d996))
* **admin:** an import and a fixture load write only what the operator could write by hand, and a file with one row they may not write is refused whole (OR-67) ([#551](https://github.com/jcsvwinston/orbit/issues/551)) ([6d84686](https://github.com/jcsvwinston/orbit/commit/6d8468681639c3efbb895d3c44a7d347b7dbbc0d))
* **ui:** Data Studio offers a batch Delete only with bulk_delete, a read-only View to an operator without update, and an open model that reads at AA; the browser bench signs in as two partial operators (OR-64) ([#547](https://github.com/jcsvwinston/orbit/issues/547)) ([54ee0ac](https://github.com/jcsvwinston/orbit/commit/54ee0ac88b40ef7bd4f26d55cb8dbdfc80c3fb1d))
* **ui:** Data Studio offers a model, a record's History, Export, Import, Fields and a saved view's removal only to an operator the server lets through; the browser bench reads them for three partial operators (OR-65) ([#548](https://github.com/jcsvwinston/orbit/issues/548)) ([7570390](https://github.com/jcsvwinston/orbit/commit/7570390cb264ea9c201e74e4d6f43b9dc138e9fb))


### Performance

* **ui:** the panel travels compressed and within a gzip budget, admin-server stops embedding it, and the icon font and error text pass the panel's own checks (A12 O1) ([#544](https://github.com/jcsvwinston/orbit/issues/544)) ([4cfc2db](https://github.com/jcsvwinston/orbit/commit/4cfc2db707c90494bf4f82691490b26933b2e846))

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
