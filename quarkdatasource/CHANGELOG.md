# Changelog

## [1.8.21](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v1.8.20...quarkdatasource/v1.8.21) (2026-09-05)


### Fixed

* **deps:** align root, agent, server, quarkbridge, quarkdatasource to the set (nucleus v1.24.0, quark v1.11.0) ([#433](https://github.com/jcsvwinston/orbit/issues/433)) ([d3fe9b6](https://github.com/jcsvwinston/orbit/commit/d3fe9b61fe92df0e47bcc01d4afc9f8b2faf1212))

## [1.8.20](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v1.8.19...quarkdatasource/v1.8.20) (2026-09-05)


### Fixed

* **admin:** string record ids end to end, tenant scope on every Data Studio operation, 400 for search without searchable fields ([#430](https://github.com/jcsvwinston/orbit/issues/430)) ([df8676f](https://github.com/jcsvwinston/orbit/commit/df8676f02567a32ae98b3fc7a8eb192b68aa0bff))
* **fleet:** cap live streams per operator, coalesce the aggregate push, share the node matcher, bound agent commands, count real reconnects, normalise CPU, pin CSP to the host ([#427](https://github.com/jcsvwinston/orbit/issues/427)) ([6016a75](https://github.com/jcsvwinston/orbit/commit/6016a75b4ac171cd8ae2b6ee229a1a6c3bb9a5d3))

## [1.8.19](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v1.8.18...quarkdatasource/v1.8.19) (2026-09-05)


### Fixed

* **deps:** release quarkdatasource with its root pin on v1.8.24 ([#421](https://github.com/jcsvwinston/orbit/issues/421)) ([ff83b56](https://github.com/jcsvwinston/orbit/commit/ff83b56ecbe71bca888202d96e4af872c54c719a))

## [1.8.18](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v1.8.17...quarkdatasource/v1.8.18) (2026-09-04)


### Fixed

* **deps:** alinea root, agent, server, quarkbridge, quarkdatasource al set (nucleus v1.23.1, quark v1.10.1) ([#393](https://github.com/jcsvwinston/orbit/issues/393)) ([95a3347](https://github.com/jcsvwinston/orbit/commit/95a3347e055acb4cff8079d134c85bf1ed11ff8d))
* **release:** los puentes adoptan la línea 1.x ([#377](https://github.com/jcsvwinston/orbit/issues/377)) ([c586e8d](https://github.com/jcsvwinston/orbit/commit/c586e8d959369300bc68f0b4219d3952b38551ba))
* **server:** apply TLS to fleet listeners, stop leaking env in system snapshot, and validate Data Studio writes ([#380](https://github.com/jcsvwinston/orbit/issues/380)) ([f778e6a](https://github.com/jcsvwinston/orbit/commit/f778e6a2de2c724d7dc53c7643d0bcb3da422007))

## [1.8.17](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.19...quarkdatasource/v1.8.17) (2026-09-03)

> **About this version number.** This module came from the `0.x` line and
> jumps to `1.8.17` because a `Release-As` in the release train applied to
> every package in the repository. By the time it was noticed the tag was
> published and served by the Go module proxy, which is immutable, and
> `@latest` always resolves to the highest version — so going back to `0.x`
> would have left a public `go get` returning a version this set does not
> certify. The number stands; from here this module follows the `1.x` line.
> Nothing about its code or its API changed in this release.


### Fixed

* **deps:** alinea los puentes a quark v1.10.0 y corta el root con ellos ([#374](https://github.com/jcsvwinston/orbit/issues/374)) ([3c4e78d](https://github.com/jcsvwinston/orbit/commit/3c4e78d2be76925f17e579e5b799d920a0da009a))

## [0.2.19](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.18...quarkdatasource/v0.2.19) (2026-09-01)


### Fixed

* **deps:** alinea los seis módulos al set (nucleus v1.23.0, quark v1.9.0) ([#362](https://github.com/jcsvwinston/orbit/issues/362)) ([2ac266c](https://github.com/jcsvwinston/orbit/commit/2ac266c93a8597414c1130567a8218e03ef8ec5f))

## [0.2.18](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.17...quarkdatasource/v0.2.18) (2026-08-31)


### Fixed

* **deps:** alinea root, agent, server, quarkbridge, quarkdatasource al set (nucleus v1.22.0, quark v1.8.0) ([#360](https://github.com/jcsvwinston/orbit/issues/360)) ([2225b2c](https://github.com/jcsvwinston/orbit/commit/2225b2c746f8fd3452ce859536e8bffc8610588c))
* **quarkdatasource:** expón cada campo del Record bajo su columna de esquema ([#350](https://github.com/jcsvwinston/orbit/issues/350)) ([6beacba](https://github.com/jcsvwinston/orbit/commit/6beacba9e38fda1ed72b1bad54d533fdccf428ae))

## [0.2.17](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.16...quarkdatasource/v0.2.17) (2026-08-30)


### Fixed

* **deps:** alinea agent, quarkbridge y quarkdatasource al set (nucleus v1.21.0, quark v1.7.1) ([#340](https://github.com/jcsvwinston/orbit/issues/340)) ([ca8e877](https://github.com/jcsvwinston/orbit/commit/ca8e877996e60f3d6fcc4cbb29bf819feffd119d))

## [0.2.16](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.15...quarkdatasource/v0.2.16) (2026-08-30)


### Fixed

* **deps:** quarkdatasource pina orbit v1.8.10 (minor vigente), no v1.8.0 ([#335](https://github.com/jcsvwinston/orbit/issues/335)) ([42f5146](https://github.com/jcsvwinston/orbit/commit/42f514620d2811a9d7363f0484b9290b0749b685))

## [0.2.15](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.14...quarkdatasource/v0.2.15) (2026-08-29)


### Fixed

* **deps:** alinea al set de Quantum 1.23.0 (nucleus v1.17.1, quark v1.7.0) ([#313](https://github.com/jcsvwinston/orbit/issues/313)) ([b4cfa59](https://github.com/jcsvwinston/orbit/commit/b4cfa59027f8dba1e4688b86d2bb2a377ae82326))

## [0.2.14](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.13...quarkdatasource/v0.2.14) (2026-08-27)


### Fixed

* **deps:** quarkdatasource pina el root v1.8.0 y el lag tolerado deja de ser silencioso ([#278](https://github.com/jcsvwinston/orbit/issues/278)) ([a405b92](https://github.com/jcsvwinston/orbit/commit/a405b92ef4aba363794ea519bb1524323d07ed5f))

## [0.2.13](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.12...quarkdatasource/v0.2.13) (2026-08-25)


### Fixed

* **deps:** alinea los cinco módulos al set de parches ([#226](https://github.com/jcsvwinston/orbit/issues/226)) ([16c34d7](https://github.com/jcsvwinston/orbit/commit/16c34d76e956c7ec8df6cd9c221bcddac4cd43b2))

## [0.2.12](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.11...quarkdatasource/v0.2.12) (2026-08-24)


### Fixed

* **deps:** alinea los cinco módulos a nucleus v1.11.0 y quark v1.6.0 ([#199](https://github.com/jcsvwinston/orbit/issues/199)) ([5757805](https://github.com/jcsvwinston/orbit/commit/5757805e06974b6da52ab33fdca97cc39110f9f8))

## [0.2.11](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.10...quarkdatasource/v0.2.11) (2026-08-21)


### Fixed

* **deps:** alinea los requires de nucleus y quark al set certificado (nucleus v1.10.0, quark v1.5.2) ([#186](https://github.com/jcsvwinston/orbit/issues/186)) ([026cc3e](https://github.com/jcsvwinston/orbit/commit/026cc3e4a2d2d42a9ea03e754c5be6727932e60c))

## [0.2.10](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.9...quarkdatasource/v0.2.10) (2026-08-16)


### Fixed

* **release:** notas v1.6.0/v1.6.1 en el sitio y quarkdatasource sobre root v1.6.0 ([#177](https://github.com/jcsvwinston/orbit/issues/177)) ([ce467e4](https://github.com/jcsvwinston/orbit/commit/ce467e4351f1c797831756c1a30cdd5931dca15b))

## [0.2.9](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.8...quarkdatasource/v0.2.9) (2026-08-16)


### Fixed

* **deps:** re-pin a quark v1.5.0 y nucleus v1.8.0 (arco DX) ([#171](https://github.com/jcsvwinston/orbit/issues/171)) ([bd6c68f](https://github.com/jcsvwinston/orbit/commit/bd6c68fa0d83688955680a5dcbc06f99b9577ceb))

## [0.2.8](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.7...quarkdatasource/v0.2.8) (2026-08-15)


### Fixed

* **deps:** alinea quark a v1.4.1 en los bridges (arco QCD-CLI) ([#139](https://github.com/jcsvwinston/orbit/issues/139)) ([65a52da](https://github.com/jcsvwinston/orbit/commit/65a52da771227706df9ed5962deb955d4d90e70a))

## [0.2.7](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.6...quarkdatasource/v0.2.7) (2026-07-22)


### Fixed

* **deps:** alinea al set 1.9.0 — nucleus v1.5.0, quark v1.4.0 ([#126](https://github.com/jcsvwinston/orbit/issues/126)) ([02b9d2e](https://github.com/jcsvwinston/orbit/commit/02b9d2ebb5b926a5f04ea0d3f8c867ef92a13958))

## [0.2.6](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.5...quarkdatasource/v0.2.6) (2026-07-20)


### Fixed

* **deps:** lags cross-repo a cero — nucleus v1.4.0, quark v1.3.3, root v1.4.3 en quarkdatasource ([#116](https://github.com/jcsvwinston/orbit/issues/116)) ([a38935c](https://github.com/jcsvwinston/orbit/commit/a38935c11e817e12e045cdb96028b798ab5e412c))

## [0.2.5](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.4...quarkdatasource/v0.2.5) (2026-07-19)


### Fixed

* **quarkbridge,quarkdatasource:** alinea el require de quark con el certificado (v1.3.1) ([4f3e891](https://github.com/jcsvwinston/orbit/commit/4f3e891672458579a53d9e6bd38026387d8845a6))
* requires de quark alineados con el certificado en los módulos puente (QM6-1) ([dd60bb7](https://github.com/jcsvwinston/orbit/commit/dd60bb7c9d37d4413e7fd058f1e7029e04ab3b81))

## [0.2.4](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.3...quarkdatasource/v0.2.4) (2026-07-19)


### Fixed

* pins internos alineados con los últimos tags + guards (OR5-1, OR5-3) ([bf1dedc](https://github.com/jcsvwinston/orbit/commit/bf1dedc4440e68be373f5dd71fc4034f770042b0))
* **quarkdatasource:** alinea el require del root con el último tag (v1.4.1) ([737bfe0](https://github.com/jcsvwinston/orbit/commit/737bfe0d96a104849c693bc4ac9609ec9f82f259))

## [0.2.3](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.2...quarkdatasource/v0.2.3) (2026-07-15)


### Fixed

* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))

## [0.2.2](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.1...quarkdatasource/v0.2.2) (2026-07-12)


### Fixed

* **deps:** los puentes suben quark de v1.1.5 a v1.2.1, el quark del set certificado (H-U5) ([34cbc2e](https://github.com/jcsvwinston/orbit/commit/34cbc2ef42475de7c46a055a04a428c62a615395))
* **deps:** los puentes suben quark de v1.1.5 a v1.2.1, el quark del set certificado (H-U5) ([f4cc128](https://github.com/jcsvwinston/orbit/commit/f4cc1284c6471a12d4596121b9ea3ca59a64cb2f))

## [0.2.1](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.2.0...quarkdatasource/v0.2.1) (2026-07-11)


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/quarkdatasource/v0.1.0...quarkdatasource/v0.2.0) (2026-07-10)


### ⚠ BREAKING CHANGES

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16))

### Chore

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16)) ([b994b09](https://github.com/jcsvwinston/orbit/commit/b994b096cc5bad2ee373f94e94b75baee7df6c71))

## 0.1.0 (2026-07-03)


### Added

* **quarkdatasource:** Data Studio over Quark models — 2nd datasource implementation (ADR-001, Caso 2) ([#4](https://github.com/jcsvwinston/orbit/issues/4)) ([728c79e](https://github.com/jcsvwinston/orbit/commit/728c79ee79e0dcc06d78c9b47a74fa074c455030))


### Fixed

* **quarkbridge,quarkdatasource:** depend on real tags — standalone builds unlocked ([#10](https://github.com/jcsvwinston/orbit/issues/10)) ([062b67d](https://github.com/jcsvwinston/orbit/commit/062b67dc502ee22e7ee799059da7b429fadfc0e8))
