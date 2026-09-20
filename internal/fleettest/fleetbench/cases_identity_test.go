// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsIdentity is the family that asks who an agent is. The transport
// half — TLS, mutual TLS, the shared token, the fail-closed listener — is
// what the recon expected to find and did. The other half is what a
// certificate is FOR: naming the node. Today the server verifies the
// certificate and then believes whatever node_id the agent declares, and
// neither side can be told where its certificate files live or when they
// changed.
func controlsIdentity() []control {
	return []control{
		{id: "IDENT-01", family: "identity", title: "the agent listener serves TLS: a cleartext client is refused, a TLS client negotiates h2 and is served",
			want: present, probe: probeAgentListenerTLS},
		{id: "IDENT-02", family: "identity", title: "with mutual TLS configured, a client without a certificate is refused at the handshake and a CA-signed one registers",
			want: present, probe: probeAgentListenerMTLS},
		{id: "IDENT-03", family: "identity", title: "an agent listener off loopback refuses to start with neither a token nor verified client certificates",
			want: present, probe: probeAgentListenerFailsClosed},
		{id: "IDENT-04", family: "identity", title: "a real agent connects over mutual TLS and registers; without a client certificate it never does",
			want: present, probe: probeRealAgentOverMTLS},
		{id: "IDENT-05", family: "identity", title: "the agent's client certificate can be given by configuration (file paths) rather than as a Go value",
			want: absent, note: "agent.ExtensionConfig has no certificate, key or CA file field; its TLS field is a *tls.Config " +
				"tagged koanf:\"-\" (agent/extension_config.go), so a deployment has to build the value in code from its PEM files.",
			probe: probeAgentCertFromConfig},
		{id: "IDENT-06", family: "identity", title: "the node identity is bound to the certificate: a certificate for node-a declaring node_id node-b is refused or registered as node-a",
			want: absent, note: "the server verifies the client certificate and then registers the node under the node_id the agent declares: " +
				"an agent with a certificate for node-a and node_id node-b is listed as node-b. The certificate's subject reaches " +
				"the request context (server/auth: Identity{Subject: \"agent:<CN>\"}) but AgentService.Stream never reads it " +
				"(server/services/agent_service.go builds the NodeInfo from the registration frame alone).",
			probe: probeNodeIdentityBoundToCert},
		{id: "IDENT-07", family: "identity", title: "the server's certificate rotates without a restart: a new handshake sees the new certificate",
			want: partial, note: "only the generic Go path exists: a tls.Config whose GetCertificate answers from a source the caller swaps is " +
				"honoured by the next handshake. The product offers nothing on top — server.Config takes a *tls.Config and the " +
				"binary loads --agent-cert/--agent-key once at boot (server/cmd/admin-server/main.go); no reload flag, signal or file watch.",
			probe: probeServerCertRotation},
		{id: "IDENT-08", family: "identity", title: "the agent presents a new client certificate on its next connection without a restart",
			want: partial, note: "only the generic Go path exists: a tls.Config whose GetClientCertificate answers from a source the caller swaps " +
				"is honoured when the agent reconnects (measured across a failover to a second server). The product offers nothing " +
				"on top — agent.Config.TLS is a *tls.Config, ExtensionConfig cannot name the files, and nothing watches them.",
			probe: probeAgentCertRotation},
		{id: "IDENT-09", family: "identity", title: "a shared token authenticates an agent; a wrong token is refused with one rate-limited WARN naming the remote IP",
			want: present, probe: probeSharedTokenAuth},
		{id: "IDENT-10", family: "identity", title: "/healthz answers without credentials on both listeners while the same listeners refuse an uncredentialled RPC",
			want: present, probe: probeHealthzWithoutCredentials},
	}
}
