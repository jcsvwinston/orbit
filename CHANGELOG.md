# Changelog

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
