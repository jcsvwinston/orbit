# Changelog

## [0.8.3](https://github.com/jcsvwinston/orbit/compare/server/v0.8.2...server/v0.8.3) (2026-07-19)


### Fixed

* **server:** pin de agent al tag recién cortado v0.5.3 ([d0e9f2f](https://github.com/jcsvwinston/orbit/commit/d0e9f2ff98de30672552d463b4c6c7351e1c3e16))
* **server:** pin de agent v0.5.3 ([5291433](https://github.com/jcsvwinston/orbit/commit/52914330e497041e6c90c12ee9144ab97d89270a))

## [0.8.2](https://github.com/jcsvwinston/orbit/compare/server/v0.8.1...server/v0.8.2) (2026-07-19)


### Fixed

* pins internos alineados con los últimos tags + guards (OR5-1, OR5-3) ([bf1dedc](https://github.com/jcsvwinston/orbit/commit/bf1dedc4440e68be373f5dd71fc4034f770042b0))
* **server:** alinea los pins internos con los últimos tags (agent v0.5.1, proto v0.4.1) ([f08e195](https://github.com/jcsvwinston/orbit/commit/f08e19529fee7bad4c555b6f6c20da4758da64ba))
* **server:** pin de agent al tag recién cortado v0.5.2 ([c5e47e1](https://github.com/jcsvwinston/orbit/commit/c5e47e109c61ceec1c44ad421bb8b77d4476e4a8))
* **server:** pin de agent v0.5.2 + regla de mismo-minor para la arista root ([43d312d](https://github.com/jcsvwinston/orbit/commit/43d312d3981c1524a17cdaa01082e600bf791c2e))
* **server:** WARN con IP remota al rechazar el token de un agente (OR5-2) ([39e20e0](https://github.com/jcsvwinston/orbit/commit/39e20e0010c937c99fd2488f5d6b3d60ae84005e))
* un token de agente rechazado ya no falla en silencio (OR5-2) ([f00e9e0](https://github.com/jcsvwinston/orbit/commit/f00e9e0b232e92ddf2c90b66cc377d19b0bdd751))

## [0.8.1](https://github.com/jcsvwinston/orbit/compare/server/v0.8.0...server/v0.8.1) (2026-07-15)


### Fixed

* **deps:** completa go.sum tras el bump a nucleus v1.3.1 ([7c210a1](https://github.com/jcsvwinston/orbit/commit/7c210a1bd064b35140fd34d2dd3aa4c5702ee0dc))
* **deps:** sube el pin de nucleus a v1.3.1 (trae el fix de la PK en Postgres) ([48cb244](https://github.com/jcsvwinston/orbit/commit/48cb244c5da6b091038c3c31e6fc1966f777bf4b))
* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))
* **server:** alinea el require de proto/agent con el código (GetSelf) ([414db08](https://github.com/jcsvwinston/orbit/commit/414db082a87a7f9dfdb8999ae51507a76d2bf146))

## [0.8.0](https://github.com/jcsvwinston/orbit/compare/server/v0.7.0...server/v0.8.0) (2026-07-14)


### Added

* GetSelf — versión del server e identidad del operador en la UI (OR-UX-P1-6, [#70](https://github.com/jcsvwinston/orbit/issues/70)) ([#83](https://github.com/jcsvwinston/orbit/issues/83)) ([550c691](https://github.com/jcsvwinston/orbit/commit/550c691a556f3584655c67a92b5d718d7b752d9f))
* **ui:** barra de filtros en las páginas de stream + knob de sampling (OR-UX-P1-3, [#71](https://github.com/jcsvwinston/orbit/issues/71)) ([#78](https://github.com/jcsvwinston/orbit/issues/78)) ([5e75f4b](https://github.com/jcsvwinston/orbit/commit/5e75f4b57595331b0b11f5557a089982d0228891))
* **ui:** bundle P2 del backlog fleet — NodeDetail Recent activity, búsqueda de modelos, SLOW_MS configurable (OR-UX-P2, [#74](https://github.com/jcsvwinston/orbit/issues/74)) ([#85](https://github.com/jcsvwinston/orbit/issues/85)) ([3575ea4](https://github.com/jcsvwinston/orbit/commit/3575ea449653c41cd0b6f490bd959f2f85a75cab))
* **ui:** expone en Data Studio lo que el backend ya sabe hacer (OR-UX-P1-2, [#72](https://github.com/jcsvwinston/orbit/issues/72)) ([#82](https://github.com/jcsvwinston/orbit/issues/82)) ([35964f4](https://github.com/jcsvwinston/orbit/commit/35964f4be60e1fdfb1fb8042f8e8d1ee996cdee5))
* **ui:** herramientas de revisión del audit log (OR-UX-P1-7, [#73](https://github.com/jcsvwinston/orbit/issues/73)) ([#81](https://github.com/jcsvwinston/orbit/issues/81)) ([c974f39](https://github.com/jcsvwinston/orbit/commit/c974f3908d77d679d914ae145a4b2d46428de26f))

## [0.7.0](https://github.com/jcsvwinston/orbit/compare/server/v0.6.0...server/v0.7.0) (2026-07-13)


### Added

* cierra el backlog fleet de la auditoría v1.2.1 — telemetría que se reanuda, node_id correlacionado, sampler real, snapshots, operador read-only ([#66](https://github.com/jcsvwinston/orbit/issues/66)) ([7535bbd](https://github.com/jcsvwinston/orbit/commit/7535bbd4e3587df068b71edbdcf3481ab3e4e195))
* **ui:** UX del plano fleet — toasts, feedback en Data Studio, pausa con buffer, pantalla 401, accesibilidad y contraste ([#68](https://github.com/jcsvwinston/orbit/issues/68)) ([40ab5c9](https://github.com/jcsvwinston/orbit/commit/40ab5c9e8ed4d9d789e8ac9c03eadc982734eddd))

## [0.6.0](https://github.com/jcsvwinston/orbit/compare/server/v0.5.0...server/v0.6.0) (2026-07-12)


### Added

* **server:** endurece los defaults del admin-server + correcciones del brief (H-O1..H-O4, H-O7) ([79a5595](https://github.com/jcsvwinston/orbit/commit/79a5595390829184158cf3a7f38f32690a8a289c), [d1735e1](https://github.com/jcsvwinston/orbit/commit/d1735e1b117b29d89e8ce33e1bef1692381772ee))

### Behavior changes

Endurecimiento de seguridad: un despliegue existente puede notar estos cambios al actualizar.

* El admin-server **se niega a arrancar** el listener de agentes en una interfaz no-loopback sin autenticación: hace falta `--agent-token` o TLS (`--agent-cert`/`--agent-key`). Para forzar el comportamiento anterior existe `--insecure-agent-listener`.
* El modo trusted-proxy del UI ahora exige el secreto compartido `--ui-proxy-secret`: las peticiones con `X-Auth-User` que no traigan `X-Auth-Proxy-Secret` correcto reciben `401`.

## [0.5.0](https://github.com/jcsvwinston/orbit/compare/server/v0.4.0...server/v0.5.0) (2026-07-11)


### Added

* SQL stream shows the driver-reported row count — the W2 waiver lands (v1.2 arc) ([#49](https://github.com/jcsvwinston/orbit/issues/49)) ([04071da](https://github.com/jcsvwinston/orbit/commit/04071da06776f86c61d0a0b9aac2c6c76c20e95b))


### Fixed

* **fleet:** bump proto to v0.3.0 in agent and server — standalone resolution restored after W2 ([#54](https://github.com/jcsvwinston/orbit/issues/54)) ([ea225a9](https://github.com/jcsvwinston/orbit/commit/ea225a9ae158ac43d9e51789bbf1575edf93f1c7))

## [0.4.0](https://github.com/jcsvwinston/orbit/compare/server/v0.3.1...server/v0.4.0) (2026-07-11)


### Added

* Access control and Audit log wired end-to-end — the W1 waiver lands (v1.2 arc) ([#42](https://github.com/jcsvwinston/orbit/issues/42)) ([8c600ce](https://github.com/jcsvwinston/orbit/commit/8c600ce2504b4514a2292002ea322b73ce809c55))


### Fixed

* **fleet:** bump agent to v0.3.0 in server — full standalone resolution after W1 ([#48](https://github.com/jcsvwinston/orbit/issues/48)) ([1617c0b](https://github.com/jcsvwinston/orbit/commit/1617c0bfa26024aa3b466a4fb1643727a4961680))
* **fleet:** bump proto to v0.2.0 in agent and server — standalone resolution restored after W1 ([#47](https://github.com/jcsvwinston/orbit/issues/47)) ([d8009bf](https://github.com/jcsvwinston/orbit/commit/d8009bf0990844dac57090ed17d9dda1b789f90b))

## [0.3.1](https://github.com/jcsvwinston/orbit/compare/server/v0.3.0...server/v0.3.1) (2026-07-11)


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## [0.3.0](https://github.com/jcsvwinston/orbit/compare/server/v0.2.0...server/v0.3.0) (2026-07-11)


### Added

* **server:** opt-in Prometheus /metrics listener + honest --version from build info ([#33](https://github.com/jcsvwinston/orbit/issues/33)) ([4e77621](https://github.com/jcsvwinston/orbit/commit/4e776212d58d8508151553dec21869d088c0de4e))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/server/v0.1.0...server/v0.2.0) (2026-07-10)


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
