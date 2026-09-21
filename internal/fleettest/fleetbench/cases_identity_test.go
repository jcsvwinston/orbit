// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controlsIdentity is the family that asks who an agent is. The transport
// half — TLS, mutual TLS, the shared token, the fail-closed listener — is
// what the recon expected to find and did. The other half is what a
// certificate is FOR: naming the node. Since A9 S1 the agent loads its
// certificate from files named in its configuration and registers under
// the certificate's Common Name, and a server started with
// AgentIdentityFromCertificate refuses any other node_id (opt-in: the
// default still registers the declared name, with a WARN, until the next
// major). Since S2 both sides serve their certificate FROM the files: a
// handshake that finds them changed re-reads them, so a rotation is a
// write to two files and no restart. The CA bundles are still read once.
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
			want: present, probe: probeAgentCertFromConfig},
		{id: "IDENT-06", family: "identity", title: "the node identity is bound to the certificate: a certificate for node-a declaring node_id node-b is refused or registered as node-a",
			want: present, probe: probeNodeIdentityBoundToCert},
		{id: "IDENT-07", family: "identity", title: "the server's certificate rotates without a restart: a new handshake sees the new certificate",
			want: present, probe: probeServerCertRotation},
		{id: "IDENT-08", family: "identity", title: "the agent presents a new client certificate on its next connection without a restart",
			want: present, probe: probeAgentCertRotation},
		{id: "IDENT-09", family: "identity", title: "a shared token authenticates an agent; a wrong token is refused with one rate-limited WARN naming the remote IP",
			want: present, probe: probeSharedTokenAuth},
		{id: "IDENT-10", family: "identity", title: "/healthz answers without credentials on both listeners while the same listeners refuse an uncredentialled RPC",
			want: present, probe: probeHealthzWithoutCredentials},
	}
}
