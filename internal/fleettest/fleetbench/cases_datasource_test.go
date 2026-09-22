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
			want: present, probe: probeIdentityCrossesStream},
		{id: "FDS-06", family: "datasource", title: "the application's per-model policy applies to the fleet operator: a denied model is refused",
			want: present, probe: probeAppPolicyAppliesToFleet},
		{id: "FDS-07", family: "datasource", title: "fleet reads are tenant-filtered when the model declares a tenant column",
			want: present, probe: probeTenantFilteredReads},
		{id: "FDS-08", family: "datasource", title: "filters with operators (contains, range, set, null) reach the agent",
			want: present, probe: probeFilterOperatorsOverWire},
		{id: "FDS-09", family: "datasource", title: "pagination carries an exact total, filtered or not",
			want: present, probe: probePaginationExactTotal},
		{id: "FDS-10", family: "datasource", title: "the agent serves Data Studio through the datasource contract, so a contract implementation can be registered in the fleet",
			want: present, probe: probeAgentSpeaksDatasource},
		{id: "FDS-11", family: "datasource", title: "a fleet mutation leaves an audit entry with operator, model, record and node, and says what changed",
			want: partial, note: "ListAudit returns the entry attributed to actor, action, target (model and record id) and node; " +
				"AuditEntry declares before_json and after_json (additive, with DataStudioResponse.previous for the agent to " +
				"send the old values) and the server writes nothing into them yet: it uses them once it pins the proto tag " +
				"that carries them. Whether the entry survives the process is RET-04's measurement.",
			probe: probeFleetAuditEntry},
		{id: "FDS-12", family: "datasource", title: "the fleet-consumes-the-contract decision (docs/adrs/ADR-002) is recorded as implemented in the ADR and in the index",
			want: absent, note: "the ADR's front matter says status: accepted and the index row in docs/adrs/README.md says " +
				"\"pendiente de implementar\": the decision is taken and the work is open.",
			probe: probeADR002RecordedImplemented},
	}
}
