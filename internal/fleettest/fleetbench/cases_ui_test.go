// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsUI is the family that asks whether the two web UIs are one
// product. They are two projects with no shared code: the fleet UI under
// ui/ (embedded by the server) and the panel under internal/admin/ui
// (embedded by the in-process module), with two token systems, one test
// suite, one freshness gate and one bundle budget between them — all on
// the panel's side.
func controlsUI() []control {
	return []control{
		{id: "UI-01", family: "ui", title: "one frontend project serves both planes: the server and the panel embed the same dist",
			want: present, note: "one project (ui/package.json) with two entries builds into one dist that the ui module embeds " +
				"(ui/embed.go, ADR-015); the admin server serves dist/fleet and the panel dist/panel, both through that module, " +
				"and neither embeds a dist of its own.",
			probe: probeOneFrontendProject},
		{id: "UI-02", family: "ui", title: "the two planes share design tokens: one token source imported by both, or one project",
			want: present, note: "one project: both entries' stylesheets import src/shared/tokens.css, the design system's tokens. " +
				"The fleet's screens still read their numbered palette (src/fleet/index.css) until they are re-skinned onto the " +
				"shared names; the source is one.",
			probe: probeSharedDesignTokens},
		{id: "UI-03", family: "ui", title: "the fleet UI has automated tests: a runner, a test script and at least one spec",
			want: present, note: "the one project runs vitest (npm test) over the panel's suite and the fleet's specs " +
				"(src/fleet/lib/*.test.ts); the CI lane runs it before building.",
			probe: probeFleetUITests},
		{id: "UI-04", family: "ui", title: "CI checks that the fleet UI's committed dist is fresh, as the panel's lane does",
			want: present, note: "the ui job in .github/workflows/ci.yml typechecks, lints, tests and builds the one project and " +
				"fails when the committed ui/dist — the one both binaries embed — differs from what it built.",
			probe: probeFleetDistFreshnessGate},
		{id: "UI-05", family: "ui", title: "a bundle-size budget covers the fleet UI (a test constant, a size-limit configuration or a CI step)",
			want: present, note: "ui/embed_test.go names a budget per entry (panel and fleet initial JS and CSS) and fails the ui " +
				"module's tests when the embedded dist exceeds it; the fleet's is its whole bundle today, a ceiling for the re-skin.",
			probe: probeFleetBundleBudget},
		{id: "UI-06", family: "ui", title: "the fleet UI's generated stubs are connect-es 2 / protobuf-es 2, in the dependencies and in the generators",
			want: present, note: "ui/package.json pins @connectrpc/connect ^2, @connectrpc/connect-web ^2 and @bufbuild/protobuf ^2, " +
				"and proto/buf.gen.yaml generates the fleet's stubs with bufbuild/es v2 alone: messages and service descriptors from " +
				"one generator, created with create(Schema) and called through createClient.",
			probe: probeConnectES2},
		{id: "UI-07", family: "ui", title: "the browser instrument covers the fleet UI: a Playwright spec navigates to a path outside /admin",
			want: present, note: "internal/adminbench/browser/specs/fleet.spec.ts is the fleet project of the same instrument: driven from " +
				"internal/fleettest (TestFleetBrowserBench), which boots an admin server and an agent, it opens the fleet UI at / and " +
				"measures six UIF controls (instrument, overview lists the node, contrast, names, landmarks, keyboard).",
			probe: probeBrowserInstrumentCoversFleet},
		{id: "UI-08", family: "ui", title: "the fleet UI is told the operator's role: GetSelf says read-only for a viewer",
			want: present, probe: probeUIKnowsRole},
		{id: "UI-09", family: "ui", title: "the fleet UI has a tenant notion: a message on the wire carries one and the SPA sends or shows it",
			want: partial, note: "the SPA shows the operator's tenant (SelfInfo.tenant, in the footer line) and marks a tenant-scoped " +
				"model and its column (ModelInfo.tenant_field) — both additive fields of this session — but the server and the agent " +
				"fill them once they pin the protocol that carries them; until then the UI reads fields the wire leaves empty.",
			probe: probeFleetTenantNotion},
		{id: "UI-10", family: "ui", title: "the panel's initial load stays within its budget, and the budget is a test constant",
			want: present, probe: probePanelBudgetEnforced},
	}
}
