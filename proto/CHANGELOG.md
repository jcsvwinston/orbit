# Changelog

## [0.4.1](https://github.com/jcsvwinston/orbit/compare/proto/v0.4.0...proto/v0.4.1) (2026-07-15)


### Fixed

* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))

## [0.4.0](https://github.com/jcsvwinston/orbit/compare/proto/v0.3.0...proto/v0.4.0) (2026-07-14)


### Added

* GetSelf — versión del server e identidad del operador en la UI (OR-UX-P1-6, [#70](https://github.com/jcsvwinston/orbit/issues/70)) ([#83](https://github.com/jcsvwinston/orbit/issues/83)) ([550c691](https://github.com/jcsvwinston/orbit/commit/550c691a556f3584655c67a92b5d718d7b752d9f))

## [0.3.0](https://github.com/jcsvwinston/orbit/compare/proto/v0.2.0...proto/v0.3.0) (2026-07-11)


### Added

* SQL stream shows the driver-reported row count — the W2 waiver lands (v1.2 arc) ([#49](https://github.com/jcsvwinston/orbit/issues/49)) ([04071da](https://github.com/jcsvwinston/orbit/commit/04071da06776f86c61d0a0b9aac2c6c76c20e95b))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/proto/v0.1.1...proto/v0.2.0) (2026-07-11)


### Added

* Access control and Audit log wired end-to-end — the W1 waiver lands (v1.2 arc) ([#42](https://github.com/jcsvwinston/orbit/issues/42)) ([8c600ce](https://github.com/jcsvwinston/orbit/commit/8c600ce2504b4514a2292002ea322b73ce809c55))

## [0.1.1](https://github.com/jcsvwinston/orbit/compare/proto/v0.1.0...proto/v0.1.1) (2026-07-11)


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## 0.1.0 (2026-07-10)


### ⚠ BREAKING CHANGES

* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23))

### Added

* **fleet:** agent, proto and server join release-please — the fleet leg gets tags (gate A-2) ([#22](https://github.com/jcsvwinston/orbit/issues/22)) ([be3362b](https://github.com/jcsvwinston/orbit/commit/be3362b98b57f0464f1d6cf2cc1bd936d2f5e26c))
* relocate cluster observability agent subsystem into orbit (ADR-019) ([59f2e59](https://github.com/jcsvwinston/orbit/commit/59f2e593a83b6cd22193b165cbc780c827a10514))
* **ui:** Orbit Admin redesign — 11 pantallas, dos temas, tokens del handoff ([#15](https://github.com/jcsvwinston/orbit/issues/15)) ([5cc789f](https://github.com/jcsvwinston/orbit/commit/5cc789fe50ad0b84a879183bd209b36f287b6655))


### Documentation

* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23)) ([fabc580](https://github.com/jcsvwinston/orbit/commit/fabc580046b29277d4df9a459dc27f16619c0fb9))
