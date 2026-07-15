# Changelog

## [0.3.2](https://github.com/jcsvwinston/orbit/compare/quarkbridge/v0.3.1...quarkbridge/v0.3.2) (2026-07-15)


### Fixed

* **deps:** completa go.sum tras el bump a nucleus v1.3.1 ([7c210a1](https://github.com/jcsvwinston/orbit/commit/7c210a1bd064b35140fd34d2dd3aa4c5702ee0dc))
* **deps:** sube el pin de nucleus a v1.3.1 (trae el fix de la PK en Postgres) ([48cb244](https://github.com/jcsvwinston/orbit/commit/48cb244c5da6b091038c3c31e6fc1966f777bf4b))
* **fleet:** OR-1 (server no compilaba standalone) + OR-2 (el token no viajaba en el stream) + el CI que faltaba ([9d143f1](https://github.com/jcsvwinston/orbit/commit/9d143f1a3234d829af9e5e7803545cf810444f83))
* **security:** compila con Go 1.26.5 — cierra GO-2026-5856 (crypto/tls) ([ba4ac2a](https://github.com/jcsvwinston/orbit/commit/ba4ac2aad39bd66cd082860bd08bb508eea9cf5c))

## [0.3.1](https://github.com/jcsvwinston/orbit/compare/quarkbridge/v0.3.0...quarkbridge/v0.3.1) (2026-07-12)


### Fixed

* **deps:** los puentes suben quark de v1.1.5 a v1.2.1, el quark del set certificado (H-U5) ([34cbc2e](https://github.com/jcsvwinston/orbit/commit/34cbc2ef42475de7c46a055a04a428c62a615395))
* **deps:** los puentes suben quark de v1.1.5 a v1.2.1, el quark del set certificado (H-U5) ([f4cc128](https://github.com/jcsvwinston/orbit/commit/f4cc1284c6471a12d4596121b9ea3ca59a64cb2f))

## [0.3.0](https://github.com/jcsvwinston/orbit/compare/quarkbridge/v0.2.1...quarkbridge/v0.3.0) (2026-07-11)


### Added

* SQL stream shows the driver-reported row count — the W2 waiver lands (v1.2 arc) ([#49](https://github.com/jcsvwinston/orbit/issues/49)) ([04071da](https://github.com/jcsvwinston/orbit/commit/04071da06776f86c61d0a0b9aac2c6c76c20e95b))

## [0.2.1](https://github.com/jcsvwinston/orbit/compare/quarkbridge/v0.2.0...quarkbridge/v0.2.1) (2026-07-11)


### Fixed

* pin toolchain go1.26.5 across all six modules (GO-2026-5856) ([#36](https://github.com/jcsvwinston/orbit/issues/36)) ([7f79f96](https://github.com/jcsvwinston/orbit/commit/7f79f9667d096ac561d5eb28ac1ade17359691cf))

## [0.2.0](https://github.com/jcsvwinston/orbit/compare/quarkbridge/v0.1.0...quarkbridge/v0.2.0) (2026-07-10)


### ⚠ BREAKING CHANGES

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16))

### Chore

* **deps:** repin to nucleus v1.0.0 across all modules (lockstep, QADR-0005) ([#16](https://github.com/jcsvwinston/orbit/issues/16)) ([b994b09](https://github.com/jcsvwinston/orbit/commit/b994b096cc5bad2ee373f94e94b75baee7df6c71))

## 0.1.0 (2026-07-03)


### Added

* **quarkbridge:** opt-in Quark middleware that feeds SQL to Orbit's live view ([#2](https://github.com/jcsvwinston/orbit/issues/2)) ([0b305f4](https://github.com/jcsvwinston/orbit/commit/0b305f468490056a56b65c6c1f10da5fd2438c54))


### Fixed

* **quarkbridge,quarkdatasource:** depend on real tags — standalone builds unlocked ([#10](https://github.com/jcsvwinston/orbit/issues/10)) ([062b67d](https://github.com/jcsvwinston/orbit/commit/062b67dc502ee22e7ee799059da7b429fadfc0e8))
