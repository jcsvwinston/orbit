# Changelog

## [1.5.1](https://github.com/jcsvwinston/orbit/compare/v1.5.0...v1.5.1) (2026-07-22)


### Fixed

* **ci:** estrecha la excepción root-edge a la arista root↔quarkdatasource (MAQ-3) ([#132](https://github.com/jcsvwinston/orbit/issues/132)) ([fd608ee](https://github.com/jcsvwinston/orbit/commit/fd608eec76463de11eedcb3528a92a22b8113563))
* **deps:** alinea nucleus a v1.6.0 (endurecimiento [#1](https://github.com/jcsvwinston/orbit/issues/1)) ([#134](https://github.com/jcsvwinston/orbit/issues/134)) ([8762c2b](https://github.com/jcsvwinston/orbit/commit/8762c2b4d35b4e3ee17f67d3c4731f66e6ff17b0))

## [1.5.0](https://github.com/jcsvwinston/orbit/compare/v1.4.4...v1.5.0) (2026-07-22)


### Added

* **ui:** cierra los 3 restos de UI del backlog v1.2.1 — i18n centralizado, a11y de tablas y consolidación de tablas del panel ([#123](https://github.com/jcsvwinston/orbit/issues/123)) ([6fc0332](https://github.com/jcsvwinston/orbit/commit/6fc0332b2d6076a1746ee4a052e7335592d35b11))


### Fixed

* **admin:** el feed vivo consume también SubscribeHTTP del EventBus ([#122](https://github.com/jcsvwinston/orbit/issues/122)) ([25d2e8a](https://github.com/jcsvwinston/orbit/commit/25d2e8a410cb458d624df9f1ec95e28e1cc0fcca)), closes [#121](https://github.com/jcsvwinston/orbit/issues/121)
* **ci:** la excepción root-edge tolera un minor de lag, no solo un patch ([#131](https://github.com/jcsvwinston/orbit/issues/131)) ([195fabe](https://github.com/jcsvwinston/orbit/commit/195fabe6b19e0d6358b6172552acd6f9bbad3672))
* **deps:** alinea al set 1.9.0 — nucleus v1.5.0, quark v1.4.0 ([#126](https://github.com/jcsvwinston/orbit/issues/126)) ([02b9d2e](https://github.com/jcsvwinston/orbit/commit/02b9d2ebb5b926a5f04ea0d3f8c867ef92a13958))

## [1.4.4](https://github.com/jcsvwinston/orbit/compare/v1.4.3...v1.4.4) (2026-07-20)


### Fixed

* **deps:** lags cross-repo a cero — nucleus v1.4.0, quark v1.3.3, root v1.4.3 en quarkdatasource ([#116](https://github.com/jcsvwinston/orbit/issues/116)) ([a38935c](https://github.com/jcsvwinston/orbit/commit/a38935c11e817e12e045cdb96028b798ab5e412c))
* release notes v1.4.3 con guard de contenido, sospecha de auth por endpoint y linter que veta IDs de hallazgo (OR7-1/2/3) ([#113](https://github.com/jcsvwinston/orbit/issues/113)) ([ec06b41](https://github.com/jcsvwinston/orbit/commit/ec06b41c6c6a40158e22dd3cbe24056eed111f83))

## [1.4.3](https://github.com/jcsvwinston/orbit/compare/v1.4.2...v1.4.3) (2026-07-19)


### Chore

* corta el root v1.4.3 — tag de certificación de la 6ª ronda ([ed940ff](https://github.com/jcsvwinston/orbit/commit/ed940ffd6d01b34c7032bea6497dafcc7441c892))

## [1.4.2](https://github.com/jcsvwinston/orbit/compare/v1.4.1...v1.4.2) (2026-07-19)


### Fixed

* pins internos alineados con los últimos tags + guards (OR5-1, OR5-3) ([bf1dedc](https://github.com/jcsvwinston/orbit/commit/bf1dedc4440e68be373f5dd71fc4034f770042b0))
* **server:** pin de agent al tag recién cortado v0.5.2 ([c5e47e1](https://github.com/jcsvwinston/orbit/commit/c5e47e109c61ceec1c44ad421bb8b77d4476e4a8))
* **server:** pin de agent v0.5.2 + regla de mismo-minor para la arista root ([43d312d](https://github.com/jcsvwinston/orbit/commit/43d312d3981c1524a17cdaa01082e600bf791c2e))

## [1.4.1](https://github.com/jcsvwinston/orbit/compare/v1.4.0...v1.4.1) (2026-07-15)


### Fixed

* **deps:** completa go.sum tras el bump a nucleus v1.3.1 ([7c210a1](https://github.com/jcsvwinston/orbit/commit/7c210a1bd064b35140fd34d2dd3aa4c5702ee0dc))
* **deps:** sube el pin de nucleus a v1.3.1 (trae el fix de la PK en Postgres) ([48cb244](https://github.com/jcsvwinston/orbit/commit/48cb244c5da6b091038c3c31e6fc1966f777bf4b))
* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))

## [1.4.0](https://github.com/jcsvwinston/orbit/compare/v1.3.0...v1.4.0) (2026-07-14)


### Added

* GetSelf — versión del server e identidad del operador en la UI (OR-UX-P1-6, [#70](https://github.com/jcsvwinston/orbit/issues/70)) ([#83](https://github.com/jcsvwinston/orbit/issues/83)) ([550c691](https://github.com/jcsvwinston/orbit/commit/550c691a556f3584655c67a92b5d718d7b752d9f))
* **ui:** barra de filtros en las páginas de stream + knob de sampling (OR-UX-P1-3, [#71](https://github.com/jcsvwinston/orbit/issues/71)) ([#78](https://github.com/jcsvwinston/orbit/issues/78)) ([5e75f4b](https://github.com/jcsvwinston/orbit/commit/5e75f4b57595331b0b11f5557a089982d0228891))
* **ui:** bundle P2 del backlog fleet — NodeDetail Recent activity, búsqueda de modelos, SLOW_MS configurable (OR-UX-P2, [#74](https://github.com/jcsvwinston/orbit/issues/74)) ([#85](https://github.com/jcsvwinston/orbit/issues/85)) ([3575ea4](https://github.com/jcsvwinston/orbit/commit/3575ea449653c41cd0b6f490bd959f2f85a75cab))
* **ui:** expone en Data Studio lo que el backend ya sabe hacer (OR-UX-P1-2, [#72](https://github.com/jcsvwinston/orbit/issues/72)) ([#82](https://github.com/jcsvwinston/orbit/issues/82)) ([35964f4](https://github.com/jcsvwinston/orbit/commit/35964f4be60e1fdfb1fb8042f8e8d1ee996cdee5))
* **ui:** herramientas de revisión del audit log (OR-UX-P1-7, [#73](https://github.com/jcsvwinston/orbit/issues/73)) ([#81](https://github.com/jcsvwinston/orbit/issues/81)) ([c974f39](https://github.com/jcsvwinston/orbit/commit/c974f3908d77d679d914ae145a4b2d46428de26f))

## [1.3.0](https://github.com/jcsvwinston/orbit/compare/v1.2.1...v1.3.0) (2026-07-13)


### Added

* **ui:** UX del plano fleet — toasts, feedback en Data Studio, pausa con buffer, pantalla 401, accesibilidad y contraste ([#68](https://github.com/jcsvwinston/orbit/issues/68)) ([40ab5c9](https://github.com/jcsvwinston/orbit/commit/40ab5c9e8ed4d9d789e8ac9c03eadc982734eddd))


### Fixed

* **admin:** backlog del panel in-process — audit real bajo auth, redacción, lockout de login, CSRF, headers, y los dos botones fake (terminate/export) ([#67](https://github.com/jcsvwinston/orbit/issues/67)) ([607246d](https://github.com/jcsvwinston/orbit/commit/607246d13464b7ded042fb12c8fa9d326c6165a3))

## [1.2.1](https://github.com/jcsvwinston/orbit/compare/v1.2.0...v1.2.1) (2026-07-12)


### Fixed

* **admin:** parametriza el INSERT del bootstrap admin (H-O5) ([d8bf01b](https://github.com/jcsvwinston/orbit/commit/d8bf01bc72bb32e24d3c917f16a123f1aa3178d7))
* **admin:** parametriza el INSERT del bootstrap admin (H-O5) ([65763a8](https://github.com/jcsvwinston/orbit/commit/65763a82c6825dd5e728ee99b14841488bfde2af))

## [1.2.0](https://github.com/jcsvwinston/orbit/compare/v1.1.0...v1.2.0) (2026-07-11)


### Added

* Access control and Audit log wired end-to-end — the W1 waiver lands (v1.2 arc) ([#42](https://github.com/jcsvwinston/orbit/issues/42)) ([8c600ce](https://github.com/jcsvwinston/orbit/commit/8c600ce2504b4514a2292002ea322b73ce809c55))
* SQL stream shows the driver-reported row count — the W2 waiver lands (v1.2 arc) ([#49](https://github.com/jcsvwinston/orbit/issues/49)) ([04071da](https://github.com/jcsvwinston/orbit/commit/04071da06776f86c61d0a0b9aac2c6c76c20e95b))


### Fixed

* **fleet:** bump agent to v0.3.0 in server — full standalone resolution after W1 ([#48](https://github.com/jcsvwinston/orbit/issues/48)) ([1617c0b](https://github.com/jcsvwinston/orbit/commit/1617c0bfa26024aa3b466a4fb1643727a4961680))
* **fleet:** bump proto to v0.2.0 in agent and server — standalone resolution restored after W1 ([#47](https://github.com/jcsvwinston/orbit/issues/47)) ([d8009bf](https://github.com/jcsvwinston/orbit/commit/d8009bf0990844dac57090ed17d9dda1b789f90b))
* **fleet:** bump proto to v0.3.0 in agent and server — standalone resolution restored after W2 ([#54](https://github.com/jcsvwinston/orbit/issues/54)) ([ea225a9](https://github.com/jcsvwinston/orbit/commit/ea225a9ae158ac43d9e51789bbf1575edf93f1c7))

## [1.1.0](https://github.com/jcsvwinston/orbit/compare/v1.0.0...v1.1.0) (2026-07-11)


### Added

* **server:** opt-in Prometheus /metrics listener + honest --version from build info ([#33](https://github.com/jcsvwinston/orbit/issues/33)) ([4e77621](https://github.com/jcsvwinston/orbit/commit/4e776212d58d8508151553dec21869d088c0de4e))


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## [1.0.0](https://github.com/jcsvwinston/orbit/compare/v0.3.0...v1.0.0) (2026-07-10)


### ⚠ BREAKING CHANGES

* **fleet:** none for consumers — this is what makes the modules consumable outside the repo in the first place; the marker records the dependency-graph shift from replace-wiring to tags.
* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23))
* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16))

### Added

* **contracts:** freeze the public API — the datasource contract is final (gate A-1) ([#21](https://github.com/jcsvwinston/orbit/issues/21)) ([cbf1df9](https://github.com/jcsvwinston/orbit/commit/cbf1df9e2b941722d1a7357094f2460563b63d7f))
* **fleet:** agent, proto and server join release-please — the fleet leg gets tags (gate A-2) ([#22](https://github.com/jcsvwinston/orbit/issues/22)) ([be3362b](https://github.com/jcsvwinston/orbit/commit/be3362b98b57f0464f1d6cf2cc1bd936d2f5e26c))


### Fixed

* **fleet:** drop the intra-repo replace directives — agent and server resolve by tags (gate A-2) ([#27](https://github.com/jcsvwinston/orbit/issues/27)) ([8b4d516](https://github.com/jcsvwinston/orbit/commit/8b4d5163dab6e2b1dc9d5041a383c3fe91b92c34))


### Documentation

* Config declared frozen (A-3) + anti-falsehood sweep of every doc surface (A-4) ([#23](https://github.com/jcsvwinston/orbit/issues/23)) ([fabc580](https://github.com/jcsvwinston/orbit/commit/fabc580046b29277d4df9a459dc27f16619c0fb9))
* **gate:** formalize the approved W1/W2 waivers — the v1.0 gate is closed ([fe5f2f6](https://github.com/jcsvwinston/orbit/commit/fe5f2f699b0330b1d62ab879aa47ce361f5482a4))


### Chore

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16)) ([b994b09](https://github.com/jcsvwinston/orbit/commit/b994b096cc5bad2ee373f94e94b75baee7df6c71))

## [0.3.0](https://github.com/jcsvwinston/orbit/compare/v0.2.0...v0.3.0) (2026-07-06)


### Added

* **ui:** Orbit Admin redesign — 11 pantallas, dos temas, tokens del handoff ([#15](https://github.com/jcsvwinston/orbit/issues/15)) ([5cc789f](https://github.com/jcsvwinston/orbit/commit/5cc789fe50ad0b84a879183bd209b36f287b6655))


### Fixed

* **quarkbridge,quarkdatasource:** depend on real tags — standalone builds unlocked ([#10](https://github.com/jcsvwinston/orbit/issues/10)) ([062b67d](https://github.com/jcsvwinston/orbit/commit/062b67dc502ee22e7ee799059da7b429fadfc0e8))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/v0.1.0...v0.2.0) (2026-07-03)


### Added

* **datastudio:** decouple Data Studio behind a neutral datasource contract (ADR-001) ([#3](https://github.com/jcsvwinston/orbit/issues/3)) ([782b388](https://github.com/jcsvwinston/orbit/commit/782b388c93f80bee5bc53758ae912355171f9196))
* **quarkbridge:** opt-in Quark middleware that feeds SQL to Orbit's live view ([#2](https://github.com/jcsvwinston/orbit/issues/2)) ([0b305f4](https://github.com/jcsvwinston/orbit/commit/0b305f468490056a56b65c6c1f10da5fd2438c54))
* **quarkdatasource:** Data Studio over Quark models — 2nd datasource implementation (ADR-001, Caso 2) ([#4](https://github.com/jcsvwinston/orbit/issues/4)) ([728c79e](https://github.com/jcsvwinston/orbit/commit/728c79ee79e0dcc06d78c9b47a74fa074c455030))


### Fixed

* **ci:** pin the bridges' first release to 0.1.0 (release-as) ([#9](https://github.com/jcsvwinston/orbit/issues/9)) ([76984b0](https://github.com/jcsvwinston/orbit/commit/76984b049a02bfd8490eb3cfd7e13cfe94425f16))
