# Changelog

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
