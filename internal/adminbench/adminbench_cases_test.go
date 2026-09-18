// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

// controls is the bench: every capability the admin surface is measured on,
// with the verdict this repository RECORDS for it.
//
// The list is a policy choice, not a derivation of what happens to exist. It
// comes from what an operator needs from an admin product, taken from the
// products that already answer it — Django Admin, Filament/Nova,
// Directus/Payload, React-Admin/Refine — which is the same comparison the
// maturity audit of 2026-09-03 scored this repository against.
//
// What is deliberately NOT here: the fleet plane (a separate product surface,
// and A9's subject), and the qualities that only exist in a browser —
// contrast, focus order, keyboard reach, whether a toast can be dismissed.
// Those are real and unmeasured; they need an instrument that runs in a
// browser, and asserting them from Go would be a claim, not a measurement.
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving one
// is a deliberate edit in the change that moves the code.
func controls() []control {
	return []control{
		// ---- data studio -------------------------------------------------
		{id: "DS-01", family: "data-studio", title: "models are discovered from the host registry",
			want: present, probe: probeModelList},
		{id: "DS-02", family: "data-studio", title: "a record survives create, read, update and delete",
			want: present, probe: probeCRUD},
		{id: "DS-03", family: "data-studio", title: "model validation refuses a bad record and names the field",
			want: present, probe: probeValidation},
		{id: "DS-04", family: "data-studio", title: "pagination carries a total a pager can use",
			want: present, probe: probePagination},
		{id: "DS-05", family: "data-studio", title: "filters with operators (range, contains, in, null)",
			want: present, probe: probeFilterOperators},
		{id: "DS-06", family: "data-studio", title: "search over the fields the model marks searchable",
			want: present, probe: probeSearch},
		{id: "DS-07", family: "data-studio", title: "ordering done by the server, so it holds across pages",
			want: present, probe: probeOrdering},
		{id: "DS-08", family: "data-studio", title: "bulk actions over a selection",
			want: present, probe: probeBulkActions},
		{id: "DS-09", family: "data-studio", title: "actions an application defines for its own models",
			want: present, probe: probeCustomActions},
		{id: "DS-10", family: "data-studio", title: "a form can resolve what a foreign key points at",
			want: present, probe: probeRelationLookup},
		{id: "DS-11", family: "data-studio", title: "nested and many-to-many editing (inlines)",
			want: present, note: "the parent and its children in one write; not transactional, and the response says what each child did",
			probe: probeNestedEditing},
		{id: "DS-12", family: "data-studio", title: "rich field types (document, file, rich text)",
			want: present, probe: probeFieldTypes},
		{id: "DS-13", family: "data-studio", title: "export the selected table",
			want: present, probe: probeExport},
		{id: "DS-14", family: "data-studio", title: "import a file: upload, validate, execute",
			want: present, probe: probeImport},
		{id: "DS-15", family: "data-studio", title: "fixtures: dump and load a data set",
			want: present, probe: probeFixtures},
		{id: "DS-16", family: "data-studio", title: "record history: what this row said before",
			want: present, note: "the audit trail read by record: it goes back as far as the trail does, which is now the retention window and not the process",
			probe: probeRecordHistory},
		{id: "DS-17", family: "data-studio", title: "saved views an operator returns to",
			want: present, probe: probeSavedViews},

		// ---- permissions -------------------------------------------------
		{id: "PERM-01", family: "permissions", title: "permissions per model and action",
			want: present, probe: probeModelPermissions},
		{id: "PERM-02", family: "permissions", title: "operators are created and granted from the panel",
			want: present, probe: probeAdminUserManagement},
		{id: "PERM-03", family: "permissions", title: "an operator's credentials can be reset or revoked from the panel",
			want: present, probe: probeOperatorCredentialReset},
		{id: "PERM-04", family: "permissions", title: "roles assigned and removed from the panel",
			want: present, probe: probeRolesFromPanel},
		{id: "PERM-05", family: "permissions", title: "policies listed, added and removed from the panel",
			want: present, probe: probePolicyManagement},
		{id: "PERM-06", family: "permissions", title: "permissions per field",
			want: present, probe: probeFieldPermissions},
		{id: "PERM-07", family: "permissions", title: "permissions per row (an operator's own records)",
			want: present, probe: probeRowPermissions},
		{id: "PERM-08", family: "permissions", title: "a read-only operator can look and cannot write",
			want: present, probe: probeReadOnlyOperator},
		{id: "PERM-09", family: "permissions", title: "the UI can tell what this operator may do",
			want: present, probe: probeDeniedActionVisible},

		// ---- audit -------------------------------------------------------
		{id: "AUD-01", family: "audit", title: "a write to a record leaves a trace naming the operator",
			want: present, probe: probeAuditWrites},
		{id: "AUD-02", family: "audit", title: "the management surfaces are audited too",
			want: present, probe: probeAuditCoverage},
		{id: "AUD-03", family: "audit", title: "an update records what the row said before and after",
			want: present, probe: probeAuditValues},
		{id: "AUD-04", family: "audit", title: "secrets are redacted before they reach the trail",
			want: present, probe: probeAuditRedaction},
		{id: "AUD-05", family: "audit", title: "the trail survives a restart",
			want: present, probe: probeAuditPersistence},
		{id: "AUD-06", family: "audit", title: "the trail can be exported",
			want: present, probe: probeAuditExport},
		{id: "AUD-07", family: "audit", title: "retention is something an operator can state",
			want: present, probe: probeAuditRetention},

		// ---- operations --------------------------------------------------
		{id: "OPS-01", family: "operations", title: "active sessions are listed",
			want: present, probe: probeSessionList},
		{id: "OPS-02", family: "operations", title: "a session row says which device it is",
			want: present, probe: probeSessionDevice},
		{id: "OPS-03", family: "operations", title: "one session can be revoked from the panel",
			want: present, probe: probeSessionRevoke},
		{id: "OPS-04", family: "operations", title: "every session of one account can be revoked at once",
			want: present, probe: probeSessionRevokeAll},
		{id: "OPS-05", family: "operations", title: "live request and SQL feed",
			want: present, probe: probeLiveFeed},
		{id: "OPS-06", family: "operations", title: "the live feed streams over a websocket",
			want: present, probe: probeLiveStream},
		{id: "OPS-07", family: "operations", title: "runtime pulse: goroutines, memory, pool",
			want: present, probe: probeSystemPulse},
		{id: "OPS-08", family: "operations", title: "health of the application",
			want: present, probe: probeHealth},
		{id: "OPS-09", family: "operations", title: "migrations listed and applied",
			want: present, probe: probeMigrations},
		{id: "OPS-10", family: "operations", title: "feature flags toggled from the panel",
			want: present, probe: probeFeatureFlags},
		{id: "OPS-11", family: "operations", title: "cache inspected and flushed",
			want: present, probe: probeCache},
		{id: "OPS-12", family: "operations", title: "stored files browsed",
			want: present, probe: probeStorageBrowse},
		{id: "OPS-13", family: "operations", title: "mail delivery state is visible",
			want: present, probe: probeEmailOutbox},
		{id: "OPS-14", family: "operations", title: "job queues listed and acted on",
			want: present, probe: probeJobQueues},
		{id: "OPS-15", family: "operations", title: "an export too big for a request runs as a job",
			want: present, probe: probeAsyncExport},
		{id: "OPS-16", family: "operations", title: "a session row says whose session it is",
			want: present, probe: probeSessionOwner},
		{id: "OPS-17", family: "operations", title: "the migrations view degrades when there is nothing to list",
			want: present, probe: probeMigrationsMissingDirectory},

		// ---- customization -----------------------------------------------
		{id: "CUST-01", family: "customization", title: "the panel carries the application's title",
			want: present, probe: probeTitle},
		{id: "CUST-02", family: "customization", title: "branding: logo, colours, favicon",
			want: present, probe: probeBranding},
		{id: "CUST-03", family: "customization", title: "dashboards and widgets an application declares",
			want: present, probe: probeDashboardWidgets},
		{id: "CUST-04", family: "customization", title: "an application can add its own screen or control",
			want: present, probe: probeUIExtension},
		{id: "CUST-05", family: "customization", title: "the panel can speak another language",
			want: present, probe: probeI18n},
		{id: "CUST-06", family: "customization", title: "the panel browses a backend that is not the framework's",
			want: present, probe: probeCustomDataSource},
		{id: "CUST-07", family: "customization", title: "multi-tenant confinement",
			want: present, probe: probeMultiTenant},

		// ---- interface ---------------------------------------------------
		{id: "UI-01", family: "interface", title: "the panel ships a built interface, not just an API",
			want: present, probe: probeServesUI},
		{id: "UI-02", family: "interface", title: "the served document declares its language",
			want: present, probe: probeUILanguageTag},
	}
}
