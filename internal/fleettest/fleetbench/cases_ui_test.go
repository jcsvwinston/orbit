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
			want: absent, note: "two projects: ui/package.json builds into server/ui/dist (embedded by server/ui/embed.go) and " +
				"internal/admin/ui/package.json builds into internal/admin/ui/dist (embedded by internal/admin/ui_fallback.go).",
			probe: probeOneFrontendProject},
		{id: "UI-02", family: "ui", title: "the two planes share design tokens: one token source imported by both, or one project",
			want: absent, note: "ui/src/index.css declares numbered --t0…--t53 custom properties and internal/admin/ui/src/index.css " +
				"declares HSL shadcn-style ones; neither stylesheet nor tailwind config imports a file the other one does, " +
				"relative or through a package that resolves inside the repository.",
			probe: probeSharedDesignTokens},
		{id: "UI-03", family: "ui", title: "the fleet UI has automated tests: a runner, a test script and at least one spec",
			want: absent, note: "ui/package.json has no test script and no test runner among its devDependencies, and no " +
				"*.test.* or *.spec.* file exists under ui/; the panel has vitest and a test suite.",
			probe: probeFleetUITests},
		{id: "UI-04", family: "ui", title: "CI checks that the fleet UI's committed dist is fresh, as the panel's lane does",
			want: absent, note: "the ui job in .github/workflows/ci.yml runs typecheck, lint and build in ui/ and never diffs " +
				"server/ui/dist afterwards; the admin-ui job does exactly that for internal/admin/ui/dist.",
			probe: probeFleetDistFreshnessGate},
		{id: "UI-05", family: "ui", title: "a bundle-size budget covers the fleet UI (a test constant, a size-limit configuration or a CI step)",
			want: absent, note: "server/ui has no test file naming a byte budget (a constant like `<name>Budget = N * 1024`), " +
				"ui/package.json has no size-limit configuration or tooling, and no CI job in ui/ enforces a size; the only " +
				"budget in the repository is the panel's (internal/admin/ui_embed_test.go).",
			probe: probeFleetBundleBudget},
		{id: "UI-06", family: "ui", title: "the fleet UI's generated stubs are connect-es 2 / protobuf-es 2, in the dependencies and in the generators",
			want: absent, note: "ui/package.json pins @connectrpc/connect ^1.6.1, @connectrpc/connect-web ^1.7.0 and " +
				"@bufbuild/protobuf ^1.10.0, and proto/buf.gen.yaml pins the generators bufbuild/es:v1.10.0 and connectrpc/es:v1.6.1.",
			probe: probeConnectES2},
		{id: "UI-07", family: "ui", title: "the browser instrument covers the fleet UI: a Playwright spec navigates to a path outside /admin",
			want: absent, note: "the only Playwright spec (internal/adminbench/browser/specs/panel.spec.ts) navigates to /admin paths only; " +
				"nothing opens the fleet UI in a browser.",
			probe: probeBrowserInstrumentCoversFleet},
		{id: "UI-08", family: "ui", title: "the fleet UI is told the operator's role: GetSelf says read-only for a viewer",
			want: present, probe: probeUIKnowsRole},
		{id: "UI-09", family: "ui", title: "the fleet UI has a tenant notion: a message on the wire carries one and the SPA sends or shows it",
			want: partial, note: "the wire carries one since A9 S3 (OperatorIdentity.tenant on DataStudioRequest, server to agent), " +
				"but nothing the fleet SPA sends (ui/src, outside src/gen) names a tenant and no screen shows which tenant an " +
				"operator or a row belongs to: the Control surface still has no tenant field at all. S10 puts the tenant in the UI.",
			probe: probeFleetTenantNotion},
		{id: "UI-10", family: "ui", title: "the panel's initial load stays within its budget, and the budget is a test constant",
			want: present, probe: probePanelBudgetEnforced},
	}
}
