// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsDatasource is the family that asks whether the fleet's data
// screen speaks the same contract as the panel's — the neutral datasource
// contract the panel has used since its own Data Studio was made
// storage-agnostic, and which docs/adrs/ADR-002 decided the fleet would
// consume. The transport and the server-side gates (allowlist, viewer
// role) are present; what the contract adds — who is asking, what the
// application's policy says, which tenant, filters with operators, an
// exact total — is not on the wire.
func controlsDatasource() []control {
	return []control{
		{id: "FDS-01", family: "datasource", title: "the fleet lists the models an agent registered",
			want: present, probe: probeFleetListsModels},
		{id: "FDS-02", family: "datasource", title: "a record survives create, read, update and delete through the fleet once its model is allowlisted",
			want: present, probe: probeFleetCRUD},
		{id: "FDS-03", family: "datasource", title: "mutations are refused by default; the allowlist opens a model; reads are never gated",
			want: present, probe: probeMutationsDenyByDefault},
		{id: "FDS-04", family: "datasource", title: "a viewer operator (read-only role) can read and cannot mutate",
			want: present, probe: probeViewerCannotMutate},
		{id: "FDS-05", family: "datasource", title: "the operator identity crosses the stream: the agent-side handler is told who is asking",
			want: absent, note: "DataStudioRequest and every request body it wraps (proto/nucleus/admin/v1/admin.proto) carry no " +
				"subject, operator or identity field, and a BeforeCreate hook on the model sees no framework identity " +
				"(auth.ClaimsFromContext) when the fleet operator writes through it; the agent executes with its own database " +
				"access and the server's resolved operator stays on the server (it reaches the audit ring, not the stream).",
			probe: probeIdentityCrossesStream},
		{id: "FDS-06", family: "datasource", title: "the application's per-model policy applies to the fleet operator: a denied model is refused",
			want: absent, note: "an agent whose Authorizer denies every action on the model still answers ListRecords with rows: " +
				"agent/datastudio builds a model.CRUD on the database handle and never consults the Authorizer " +
				"(the Authorizer feeds the read-only RBAC snapshot the fleet UI displays, not enforcement).",
			probe: probeAppPolicyAppliesToFleet},
		{id: "FDS-07", family: "datasource", title: "fleet reads are tenant-filtered when the model declares a tenant column",
			want: absent, note: "a model with a declared tenant column answers every tenant's rows to an operator scoped to one: " +
				"no surface carries the tenant — server.Config has no tenant header, no request message has a tenant field, " +
				"and the agent-side handler runs with no tenant in its context (agent/datastudio, security model).",
			probe: probeTenantFilteredReads},
		{id: "FDS-08", family: "datasource", title: "filters with operators (contains, range, set, null) reach the agent",
			want: partial, note: "ListRecordsRequest.filters is a map<string,string> the agent applies as column = value, so equality " +
				"narrows the page; an operator spelling (Title__contains) is not a column, is dropped in silence and every row " +
				"comes back. The typed operator filters the model layer accepts (QueryOpts.Where) have no field on the wire.",
			probe: probeFilterOperatorsOverWire},
		{id: "FDS-09", family: "datasource", title: "pagination carries an exact total, filtered or not",
			want: absent, note: "PaginatedRecords declares total and total_estimated, and every page answers total -1 with " +
				"total_estimated true, filtered or not: the agent never asks the model layer for a count " +
				"(QueryOpts.ExactTotal is not set in agent/datastudio), so no fleet pager can say how many pages there are.",
			probe: probePaginationExactTotal},
		{id: "FDS-10", family: "datasource", title: "the agent serves Data Studio through the datasource contract, so a contract implementation can be registered in the fleet",
			want: absent, note: "agent/go.mod does not require the root module that owns the datasource package, and agent.Config " +
				"has no field typed from it: the agent builds its own model.CRUD path (agent/datastudio) and a contract " +
				"implementation such as quarkdatasource cannot be handed to it.",
			probe: probeAgentSpeaksDatasource},
		{id: "FDS-11", family: "datasource", title: "a fleet mutation leaves an audit entry with operator, model, record and node, and says what changed",
			want: partial, note: "ListAudit returns the entry attributed to actor, action, target (model and record id) and node; " +
				"AuditEntry has no before/after values. Whether the entry survives the process is RET-04's measurement.",
			probe: probeFleetAuditEntry},
		{id: "FDS-12", family: "datasource", title: "the fleet-consumes-the-contract decision (docs/adrs/ADR-002) is recorded as implemented in the ADR and in the index",
			want: absent, note: "the ADR's front matter says status: accepted and the index row in docs/adrs/README.md says " +
				"\"pendiente de implementar\": the decision is taken and the work is open.",
			probe: probeADR002RecordedImplemented},
	}
}
