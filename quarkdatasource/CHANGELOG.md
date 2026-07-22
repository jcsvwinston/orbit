# Changelog

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
