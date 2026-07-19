# Changelog

## [0.5.2](https://github.com/jcsvwinston/orbit/compare/agent/v0.5.1...agent/v0.5.2) (2026-07-19)


### Fixed

* **agent:** alinea el pin de proto con el último tag (v0.4.1) ([9c56b2f](https://github.com/jcsvwinston/orbit/commit/9c56b2f10e3230dc0b34b647d6259a8cf2a70c8d))
* **agent:** un token rechazado ya no reintenta a ~1/s con logs de «connected» (OR5-2) ([8fd63a1](https://github.com/jcsvwinston/orbit/commit/8fd63a19f579ff14346a42283c1361bd3f2a4a90))
* pins internos alineados con los últimos tags + guards (OR5-1, OR5-3) ([bf1dedc](https://github.com/jcsvwinston/orbit/commit/bf1dedc4440e68be373f5dd71fc4034f770042b0))
* un token de agente rechazado ya no falla en silencio (OR5-2) ([f00e9e0](https://github.com/jcsvwinston/orbit/commit/f00e9e0b232e92ddf2c90b66cc377d19b0bdd751))

## [0.5.1](https://github.com/jcsvwinston/orbit/compare/agent/v0.5.0...agent/v0.5.1) (2026-07-15)


### Fixed

* **agent:** adjunta el bearer token también en el stream bidi ([9df61e0](https://github.com/jcsvwinston/orbit/commit/9df61e00d36aca32c8cebe4a231c7bcccabe3224))
* **deps:** completa go.sum tras el bump a nucleus v1.3.1 ([7c210a1](https://github.com/jcsvwinston/orbit/commit/7c210a1bd064b35140fd34d2dd3aa4c5702ee0dc))
* **deps:** sube el pin de nucleus a v1.3.1 (trae el fix de la PK en Postgres) ([48cb244](https://github.com/jcsvwinston/orbit/commit/48cb244c5da6b091038c3c31e6fc1966f777bf4b))
* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))

## [0.5.0](https://github.com/jcsvwinston/orbit/compare/agent/v0.4.0...agent/v0.5.0) (2026-07-13)


### Added

* cierra el backlog fleet de la auditoría v1.2.1 — telemetría que se reanuda, node_id correlacionado, sampler real, snapshots, operador read-only ([#66](https://github.com/jcsvwinston/orbit/issues/66)) ([7535bbd](https://github.com/jcsvwinston/orbit/commit/7535bbd4e3587df068b71edbdcf3481ab3e4e195))


### Fixed

* **admin:** backlog del panel in-process — audit real bajo auth, redacción, lockout de login, CSRF, headers, y los dos botones fake (terminate/export) ([#67](https://github.com/jcsvwinston/orbit/issues/67)) ([607246d](https://github.com/jcsvwinston/orbit/commit/607246d13464b7ded042fb12c8fa9d326c6165a3))

## [0.4.0](https://github.com/jcsvwinston/orbit/compare/agent/v0.3.0...agent/v0.4.0) (2026-07-11)


### Added

* SQL stream shows the driver-reported row count — the W2 waiver lands (v1.2 arc) ([#49](https://github.com/jcsvwinston/orbit/issues/49)) ([04071da](https://github.com/jcsvwinston/orbit/commit/04071da06776f86c61d0a0b9aac2c6c76c20e95b))


### Fixed

* **fleet:** bump proto to v0.3.0 in agent and server — standalone resolution restored after W2 ([#54](https://github.com/jcsvwinston/orbit/issues/54)) ([ea225a9](https://github.com/jcsvwinston/orbit/commit/ea225a9ae158ac43d9e51789bbf1575edf93f1c7))

## [0.3.0](https://github.com/jcsvwinston/orbit/compare/agent/v0.2.1...agent/v0.3.0) (2026-07-11)


### Added

* Access control and Audit log wired end-to-end — the W1 waiver lands (v1.2 arc) ([#42](https://github.com/jcsvwinston/orbit/issues/42)) ([8c600ce](https://github.com/jcsvwinston/orbit/commit/8c600ce2504b4514a2292002ea322b73ce809c55))


### Fixed

* **fleet:** bump proto to v0.2.0 in agent and server — standalone resolution restored after W1 ([#47](https://github.com/jcsvwinston/orbit/issues/47)) ([d8009bf](https://github.com/jcsvwinston/orbit/commit/d8009bf0990844dac57090ed17d9dda1b789f90b))

## [0.2.1](https://github.com/jcsvwinston/orbit/compare/agent/v0.2.0...agent/v0.2.1) (2026-07-11)


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/agent/v0.1.0...agent/v0.2.0) (2026-07-10)


### ⚠ BREAKING CHANGES

* **fleet:** none for consumers — this is what makes the modules consumable outside the repo in the first place; the marker records the dependency-graph shift from replace-wiring to tags.

### Fixed

* **fleet:** drop the intra-repo replace directives — agent and server resolve by tags (gate A-2) ([#27](https://github.com/jcsvwinston/orbit/issues/27)) ([8b4d516](https://github.com/jcsvwinston/orbit/commit/8b4d5163dab6e2b1dc9d5041a383c3fe91b92c34))

## 0.1.0 (2026-07-10)


### ⚠ BREAKING CHANGES

* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23))
* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16))

### Added

* **fleet:** agent, proto and server join release-please — the fleet leg gets tags (gate A-2) ([#22](https://github.com/jcsvwinston/orbit/issues/22)) ([be3362b](https://github.com/jcsvwinston/orbit/commit/be3362b98b57f0464f1d6cf2cc1bd936d2f5e26c))
* relocate cluster observability agent subsystem into orbit (ADR-019) ([59f2e59](https://github.com/jcsvwinston/orbit/commit/59f2e593a83b6cd22193b165cbc780c827a10514))
* **ui:** Orbit Admin redesign — 11 pantallas, dos temas, tokens del handoff ([#15](https://github.com/jcsvwinston/orbit/issues/15)) ([5cc789f](https://github.com/jcsvwinston/orbit/commit/5cc789fe50ad0b84a879183bd209b36f287b6655))


### Documentation

* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23)) ([fabc580](https://github.com/jcsvwinston/orbit/commit/fabc580046b29277d4df9a459dc27f16619c0fb9))


### Chore

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16)) ([b994b09](https://github.com/jcsvwinston/orbit/commit/b994b096cc5bad2ee373f94e94b75baee7df6c71))
