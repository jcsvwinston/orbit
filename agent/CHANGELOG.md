# Changelog

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
