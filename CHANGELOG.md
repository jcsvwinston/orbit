# Changelog

## [1.8.21](https://github.com/jcsvwinston/orbit/compare/v1.8.20...v1.8.21) (2026-09-04)


### Fixed

* **admin:** a data source that does not serve an alias means absence, not a 500, in the model listing ([#398](https://github.com/jcsvwinston/orbit/issues/398)) ([cc38016](https://github.com/jcsvwinston/orbit/commit/cc3801644ad9963f2b491c527cdd039d88df2ee1))

## [1.8.20](https://github.com/jcsvwinston/orbit/compare/v1.8.19...v1.8.20) (2026-09-04)


### Fixed

* **deps:** pin server to agent/v0.6.12 ([#396](https://github.com/jcsvwinston/orbit/issues/396)) ([cf52d1b](https://github.com/jcsvwinston/orbit/commit/cf52d1bb7a80453c1f88f41fb395defea456f2c8))

## [1.8.19](https://github.com/jcsvwinston/orbit/compare/v1.8.18...v1.8.19) (2026-09-04)


### Fixed

* **deps:** pin agent and server to the proto and agent tags cut in v1.8.18 ([#394](https://github.com/jcsvwinston/orbit/issues/394)) ([b2f9a72](https://github.com/jcsvwinston/orbit/commit/b2f9a723b15be835e0d491f639087046958f1a82))

## [1.8.18](https://github.com/jcsvwinston/orbit/compare/v1.8.17...v1.8.18) (2026-09-04)


### Fixed

* **admin-ui:** make import, pagination and JSON fields do what they say; show deny policies; add lint and tests ([#379](https://github.com/jcsvwinston/orbit/issues/379)) ([ca5e4d0](https://github.com/jcsvwinston/orbit/commit/ca5e4d06ea6b7ac88a5b1c8a63358fb60d81c296))
* **deps:** alinea root, agent, server, quarkbridge, quarkdatasource al set (nucleus v1.23.1, quark v1.10.1) ([#393](https://github.com/jcsvwinston/orbit/issues/393)) ([95a3347](https://github.com/jcsvwinston/orbit/commit/95a3347e055acb4cff8079d134c85bf1ed11ff8d))
* **release:** los puentes adoptan la línea 1.x ([#377](https://github.com/jcsvwinston/orbit/issues/377)) ([c586e8d](https://github.com/jcsvwinston/orbit/commit/c586e8d959369300bc68f0b4219d3952b38551ba))
* **server:** apply TLS to fleet listeners, stop leaking env in system snapshot, and validate Data Studio writes ([#380](https://github.com/jcsvwinston/orbit/issues/380)) ([f778e6a](https://github.com/jcsvwinston/orbit/commit/f778e6a2de2c724d7dc53c7643d0bcb3da422007))

## [1.8.17](https://github.com/jcsvwinston/orbit/compare/v1.8.16...v1.8.17) (2026-09-03)


### Fixed

* **deps:** alinea los puentes a quark v1.10.0 y corta el root con ellos ([#374](https://github.com/jcsvwinston/orbit/issues/374)) ([3c4e78d](https://github.com/jcsvwinston/orbit/commit/3c4e78d2be76925f17e579e5b799d920a0da009a))

## [1.8.16](https://github.com/jcsvwinston/orbit/compare/v1.8.15...v1.8.16) (2026-09-03)


### Fixed

* **docs:** las notas de la v1.8.16, que fuerza el corte del root con server ([#372](https://github.com/jcsvwinston/orbit/issues/372)) ([3f141f7](https://github.com/jcsvwinston/orbit/commit/3f141f794b1ae1720593ccff1657c54895724abc))

## [1.8.15](https://github.com/jcsvwinston/orbit/compare/v1.8.14...v1.8.15) (2026-09-01)


### Fixed

* **deps:** alinea los seis módulos al set (nucleus v1.23.0, quark v1.9.0) ([#362](https://github.com/jcsvwinston/orbit/issues/362)) ([2ac266c](https://github.com/jcsvwinston/orbit/commit/2ac266c93a8597414c1130567a8218e03ef8ec5f))

## [1.8.14](https://github.com/jcsvwinston/orbit/compare/v1.8.13...v1.8.14) (2026-08-31)


### Fixed

* **admin:** cierra los hallazgos de la auditoría del panel in-process ([#349](https://github.com/jcsvwinston/orbit/issues/349)) ([cd38046](https://github.com/jcsvwinston/orbit/commit/cd38046353a092833540e4296c391b872eedd2ce))
* **admin:** counts cruzados en /api/models y live feed sordo a los tipos reales del bus ([#359](https://github.com/jcsvwinston/orbit/issues/359)) ([44b8066](https://github.com/jcsvwinston/orbit/commit/44b806691e2e8a32b47593839688079566241d91))
* **deps:** alinea root, agent, server, quarkbridge, quarkdatasource al set (nucleus v1.22.0, quark v1.8.0) ([#360](https://github.com/jcsvwinston/orbit/issues/360)) ([2225b2c](https://github.com/jcsvwinston/orbit/commit/2225b2c746f8fd3452ce859536e8bffc8610588c))
* **fleet:** puertas y honestidad del plano fleet (AO-3, OH-7, AO-4) ([#353](https://github.com/jcsvwinston/orbit/issues/353)) ([dd73b8e](https://github.com/jcsvwinston/orbit/commit/dd73b8e693cdad5f8263dd1a403f4ca66fb440fa))
* **release:** repara el manifest (faltaba una coma al reconciliar la cascada) ([ffc76d6](https://github.com/jcsvwinston/orbit/commit/ffc76d638fd9ae2901d1436a74c97d9d0fe89ea5))

## [1.8.13](https://github.com/jcsvwinston/orbit/compare/v1.8.12...v1.8.13) (2026-08-30)


### Fixed

* **deps:** el root de orbit alinea nucleus v1.21.0 ([#347](https://github.com/jcsvwinston/orbit/issues/347)) ([02af124](https://github.com/jcsvwinston/orbit/commit/02af124714cca97444a3ad570b5ec427e4adb810))

## [1.8.12](https://github.com/jcsvwinston/orbit/compare/v1.8.11...v1.8.12) (2026-08-30)


### Chore

* **release:** sella el set con orbit v1.8.12 (alineación de módulos) ([4a6aa80](https://github.com/jcsvwinston/orbit/commit/4a6aa801e41e27aebd54e556df03feeefa7082c0))

## [1.8.11](https://github.com/jcsvwinston/orbit/compare/v1.8.10...v1.8.11) (2026-08-30)


### Fixed

* **admin:** el navegador de storage confina la ruta al root cuando hay Store configurado ([#334](https://github.com/jcsvwinston/orbit/issues/334)) ([a6c0f0c](https://github.com/jcsvwinston/orbit/commit/a6c0f0c00d718bc8b956fc169f97c7fe140f6d04))

## [1.8.10](https://github.com/jcsvwinston/orbit/compare/v1.8.9...v1.8.10) (2026-08-30)


### Fixed

* **deps:** alinea todos los módulos con nucleus v1.20.1 ([#326](https://github.com/jcsvwinston/orbit/issues/326)) ([a1efcf6](https://github.com/jcsvwinston/orbit/commit/a1efcf6ca3e6f88b2d2e4da3bb3ef0c4bfe62394))

## [1.8.9](https://github.com/jcsvwinston/orbit/compare/v1.8.8...v1.8.9) (2026-08-30)


### Fixed

* **deps:** sube nucleus a v1.20.0 ([#324](https://github.com/jcsvwinston/orbit/issues/324)) ([5b002df](https://github.com/jcsvwinston/orbit/commit/5b002dff7638ba1194c2c8a229f7bac457295bfa))

## [1.8.8](https://github.com/jcsvwinston/orbit/compare/v1.8.7...v1.8.8) (2026-08-30)


### Fixed

* **admin:** el duplicado del bootstrap se reconoce por código, no por el idioma del mensaje ([#320](https://github.com/jcsvwinston/orbit/issues/320)) ([074b9c4](https://github.com/jcsvwinston/orbit/commit/074b9c4464da7e06113d5dcdd442c664104e8620))
* **config:** orbit.Config lleva tags koanf y sus claves dejan de caer en silencio ([#322](https://github.com/jcsvwinston/orbit/issues/322)) ([b4b8e9f](https://github.com/jcsvwinston/orbit/commit/b4b8e9fe1efee61faaf2c4ba48a250f74c8f5f14))
* **deps:** sube nucleus a v1.19.0 ([#323](https://github.com/jcsvwinston/orbit/issues/323)) ([211b667](https://github.com/jcsvwinston/orbit/commit/211b6678227bd136b17f0c5d10080591836a53b6))

## [1.8.7](https://github.com/jcsvwinston/orbit/compare/v1.8.6...v1.8.7) (2026-08-29)


### Fixed

* **deps:** alinea al set de Quantum 1.23.0 (nucleus v1.17.1, quark v1.7.0) ([#313](https://github.com/jcsvwinston/orbit/issues/313)) ([b4cfa59](https://github.com/jcsvwinston/orbit/commit/b4cfa59027f8dba1e4688b86d2bb2a377ae82326))

## [1.8.6](https://github.com/jcsvwinston/orbit/compare/v1.8.5...v1.8.6) (2026-08-29)


### Fixed

* **deps:** alinea los módulos de orbit con nucleus v1.17.0 ([#304](https://github.com/jcsvwinston/orbit/issues/304)) ([bd1362f](https://github.com/jcsvwinston/orbit/commit/bd1362fe0a4816a3823c4f7bb31e626686a0fba3))

## [1.8.5](https://github.com/jcsvwinston/orbit/compare/v1.8.4...v1.8.5) (2026-08-29)


### Fixed

* **deps:** alinea los módulos de orbit con nucleus v1.16.1 ([#297](https://github.com/jcsvwinston/orbit/issues/297)) ([7b9ced2](https://github.com/jcsvwinston/orbit/commit/7b9ced2a165e9c67422919d551dfe77bd592c00f))
* **deps:** sube el pin de agent a v0.6.4 y documenta v1.8.5 ([#302](https://github.com/jcsvwinston/orbit/issues/302)) ([a964f36](https://github.com/jcsvwinston/orbit/commit/a964f36e52a9f383afb70fba12c91287344c44e8))

## [1.8.4](https://github.com/jcsvwinston/orbit/compare/v1.8.3...v1.8.4) (2026-08-28)


### Fixed

* **deps:** alinea los módulos de orbit con nucleus v1.16.0 ([#292](https://github.com/jcsvwinston/orbit/issues/292)) ([996fa4e](https://github.com/jcsvwinston/orbit/commit/996fa4e434445341b0bdc112ea99082bb8fd1c2b))

## [1.8.3](https://github.com/jcsvwinston/orbit/compare/v1.8.2...v1.8.3) (2026-08-28)


### Fixed

* **deps:** alinea los módulos de orbit con nucleus v1.15.1 ([#287](https://github.com/jcsvwinston/orbit/issues/287)) ([67de144](https://github.com/jcsvwinston/orbit/commit/67de144f301d044d6fa93282950270c71a5089e0))

## [1.8.2](https://github.com/jcsvwinston/orbit/compare/v1.8.1...v1.8.2) (2026-08-28)


### Fixed

* **deps:** alinea los módulos de orbit con nucleus v1.15.0 ([#282](https://github.com/jcsvwinston/orbit/issues/282)) ([d9cce91](https://github.com/jcsvwinston/orbit/commit/d9cce911ddcbf03aa446d8258ab4574cdb154ee8))

## [1.8.1](https://github.com/jcsvwinston/orbit/compare/v1.8.0...v1.8.1) (2026-08-27)


### Fixed

* **deps:** quarkdatasource pina el root v1.8.0 y el lag tolerado deja de ser silencioso ([#278](https://github.com/jcsvwinston/orbit/issues/278)) ([a405b92](https://github.com/jcsvwinston/orbit/commit/a405b92ef4aba363794ea519bb1524323d07ed5f))

## [1.8.0](https://github.com/jcsvwinston/orbit/compare/v1.7.4...v1.8.0) (2026-08-27)


### Added

* **admin:** el panel autentica por la cadena declarada, sin delegar la autorización ([#262](https://github.com/jcsvwinston/orbit/issues/262)) ([807578e](https://github.com/jcsvwinston/orbit/commit/807578e0ec72ac176f3ff19b105d7f29db259c58))

## [1.7.4](https://github.com/jcsvwinston/orbit/compare/v1.7.3...v1.7.4) (2026-08-26)


### Fixed

* **deps:** alinea a nucleus v1.13.0 ([#249](https://github.com/jcsvwinston/orbit/issues/249)) ([19d361a](https://github.com/jcsvwinston/orbit/commit/19d361a7e96d935ae1005c500c4d17b5063fa824))

## [1.7.3](https://github.com/jcsvwinston/orbit/compare/v1.7.2...v1.7.3) (2026-08-25)


### Fixed

* **ci:** el refresco de la matriz necesita checkout ([#245](https://github.com/jcsvwinston/orbit/issues/245)) ([b2eba1c](https://github.com/jcsvwinston/orbit/commit/b2eba1c0639bbeea59f1775cd2e5d9e030e52189))
* **ci:** la matriz de módulos se refresca DESPUÉS del tag ([#234](https://github.com/jcsvwinston/orbit/issues/234)) ([71081a9](https://github.com/jcsvwinston/orbit/commit/71081a9bf59f73e7b7b07a2a70e23124b4b1ed87))

## [1.7.2](https://github.com/jcsvwinston/orbit/compare/v1.7.1...v1.7.2) (2026-08-25)


### Fixed

* **deps:** alinea los cinco módulos al set de parches ([#226](https://github.com/jcsvwinston/orbit/issues/226)) ([16c34d7](https://github.com/jcsvwinston/orbit/commit/16c34d76e956c7ec8df6cd9c221bcddac4cd43b2))

## [1.7.1](https://github.com/jcsvwinston/orbit/compare/v1.7.0...v1.7.1) (2026-08-25)


### Fixed

* **orbit:** el binding modules.orbit.* surte efecto ([#222](https://github.com/jcsvwinston/orbit/issues/222)) ([2939955](https://github.com/jcsvwinston/orbit/commit/2939955741ead61bb95ed6ffae09bf9d52814165))

## [1.7.0](https://github.com/jcsvwinston/orbit/compare/v1.6.7...v1.7.0) (2026-08-25)


### Added

* **docs:** orbit versiona su documentación ([#217](https://github.com/jcsvwinston/orbit/issues/217)) ([31b30c7](https://github.com/jcsvwinston/orbit/commit/31b30c7384fe47f460740b454beb802044b48633))


### Fixed

* **ci:** la matriz de módulos deja de tener ventana rancia tras cada tag ([#215](https://github.com/jcsvwinston/orbit/issues/215)) ([cd09255](https://github.com/jcsvwinston/orbit/commit/cd0925536976def3ca1c98fb18c4e2423be3a677))


### Reverted

* **ci:** la matriz vuelve a leer solo tags ([#220](https://github.com/jcsvwinston/orbit/issues/220)) ([9f6e322](https://github.com/jcsvwinston/orbit/commit/9f6e322defacd18e348cefb0925388322be38d54))

## [1.6.7](https://github.com/jcsvwinston/orbit/compare/v1.6.6...v1.6.7) (2026-08-25)


### Fixed

* **deps:** publica la alineación a nucleus v1.12.0 ([#206](https://github.com/jcsvwinston/orbit/issues/206)) ([fd36b8f](https://github.com/jcsvwinston/orbit/commit/fd36b8fced103c2424309277a4e9cc8902a31e22))

## [1.6.6](https://github.com/jcsvwinston/orbit/compare/v1.6.5...v1.6.6) (2026-08-24)


### Fixed

* **deps:** alinea los cinco módulos a nucleus v1.11.0 y quark v1.6.0 ([#199](https://github.com/jcsvwinston/orbit/issues/199)) ([5757805](https://github.com/jcsvwinston/orbit/commit/5757805e06974b6da52ab33fdca97cc39110f9f8))

## [1.6.5](https://github.com/jcsvwinston/orbit/compare/v1.6.4...v1.6.5) (2026-08-23)


### Fixed

* **server:** pina agent/v0.5.13 — cierra la cascada interna del set 1.14.0 ([#194](https://github.com/jcsvwinston/orbit/issues/194)) ([16e8b58](https://github.com/jcsvwinston/orbit/commit/16e8b58c7ad9f9b33436b69dd605ce1da691a71e))

## [1.6.4](https://github.com/jcsvwinston/orbit/compare/v1.6.3...v1.6.4) (2026-08-23)


### Fixed

* **agent:** alinea nucleus a v1.10.0 — completa la alineación del set que v1.6.3 dejó a medias ([#191](https://github.com/jcsvwinston/orbit/issues/191)) ([7b58f03](https://github.com/jcsvwinston/orbit/commit/7b58f03c9aa69ba658f6c946fddbdbfc1c7fb0bc))

## [1.6.3](https://github.com/jcsvwinston/orbit/compare/v1.6.2...v1.6.3) (2026-08-23)


### Fixed

* **deps:** alinea los requires de nucleus y quark al set certificado (nucleus v1.10.0, quark v1.5.2) ([#186](https://github.com/jcsvwinston/orbit/issues/186)) ([026cc3e](https://github.com/jcsvwinston/orbit/commit/026cc3e4a2d2d42a9ea03e754c5be6727932e60c))

## [1.6.2](https://github.com/jcsvwinston/orbit/compare/v1.6.1...v1.6.2) (2026-08-19)


### Fixed

* **deps:** re-pin a nucleus v1.9.1 (arco SSR + outbox) ([#180](https://github.com/jcsvwinston/orbit/issues/180)) ([abb7b22](https://github.com/jcsvwinston/orbit/commit/abb7b2272f81fc8efae8eb75ba61d9d8bbe015a2))

## [1.6.1](https://github.com/jcsvwinston/orbit/compare/v1.6.0...v1.6.1) (2026-08-16)


### Fixed

* **release:** notas v1.6.0/v1.6.1 en el sitio y quarkdatasource sobre root v1.6.0 ([#177](https://github.com/jcsvwinston/orbit/issues/177)) ([ce467e4](https://github.com/jcsvwinston/orbit/commit/ce467e4351f1c797831756c1a30cdd5931dca15b))

## [1.6.0](https://github.com/jcsvwinston/orbit/compare/v1.5.4...v1.6.0) (2026-08-16)


### Added

* **dx:** arco DX de orbit — quick-start compilable, Makefile a 6 módulos y matriz de compatibilidad generada (DX-5/17/26) ([#169](https://github.com/jcsvwinston/orbit/issues/169)) ([f759f29](https://github.com/jcsvwinston/orbit/commit/f759f29c62a07ee5ae1d6b3e09d02e24cb08df38))


### Fixed

* **deps:** re-pin a quark v1.5.0 y nucleus v1.8.0 (arco DX) ([#171](https://github.com/jcsvwinston/orbit/issues/171)) ([bd6c68f](https://github.com/jcsvwinston/orbit/commit/bd6c68fa0d83688955680a5dcbc06f99b9577ceb))

## [1.5.4](https://github.com/jcsvwinston/orbit/compare/v1.5.3...v1.5.4) (2026-08-16)


### Fixed

* **deps:** alinea nucleus a v1.7.0 en root, agent, server y quarkbridge (arco QCD-FW) ([#163](https://github.com/jcsvwinston/orbit/issues/163)) ([525549b](https://github.com/jcsvwinston/orbit/commit/525549bf54127c436e4763340e1cecbd57c8627f))

## [1.5.3](https://github.com/jcsvwinston/orbit/compare/v1.5.2...v1.5.3) (2026-08-16)


### Chore

* corta el root v1.5.3 (Release-As) ([#161](https://github.com/jcsvwinston/orbit/issues/161)) ([6ba545b](https://github.com/jcsvwinston/orbit/commit/6ba545bf0689c82e03cd7ee42fa50e0094ba1270))

## [1.5.2](https://github.com/jcsvwinston/orbit/compare/v1.5.1...v1.5.2) (2026-08-16)


### Fixed

* **deps:** alinea nucleus a v1.6.1 en root, agent, server y quarkbridge (arco QCD-CLI) ([#144](https://github.com/jcsvwinston/orbit/issues/144)) ([2828aed](https://github.com/jcsvwinston/orbit/commit/2828aed324cac2c094272d2d43e32cfee152faac))
* **deps:** alinea nucleus a v1.6.2 en root, agent, server y quarkbridge (cierre del tren QCD-CLI) ([#149](https://github.com/jcsvwinston/orbit/issues/149)) ([9d9ddb5](https://github.com/jcsvwinston/orbit/commit/9d9ddb53fb592d1a4389f3fa19c5123d49052fd9))

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
