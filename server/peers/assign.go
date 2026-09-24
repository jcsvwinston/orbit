// Package peers is the server-to-server side of the fleet plane
// (ADR-014): a mesh of admin servers that share their node registries,
// relay events to each other's UI subscribers, and assign nodes among
// themselves deterministically.
package peers

import (
	"hash/fnv"
	"sort"
	"strings"
)

// Owner picks, among endpoints, the server that owns nodeID: rendezvous
// (highest-random-weight) hashing over node id and endpoint, so every
// server that knows the same set of endpoints picks the same owner, and a
// server that joins or leaves moves only the nodes it wins or loses. Empty
// input owns nothing. Endpoints are compared as given, trimmed.
func Owner(nodeID string, endpoints []string) string {
	nodeID = strings.TrimSpace(nodeID)
	var best string
	var bestScore uint64
	first := true
	seen := map[string]bool{}
	for _, ep := range endpoints {
		ep = strings.TrimSpace(ep)
		if ep == "" || seen[ep] {
			continue
		}
		seen[ep] = true
		h := fnv.New64a()
		_, _ = h.Write([]byte(nodeID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(ep))
		score := h.Sum64()
		// Ties break on the endpoint string so the answer is total.
		if first || score > bestScore || (score == bestScore && ep < best) {
			best, bestScore, first = ep, score, false
		}
	}
	return best
}

// Sorted returns a copy of endpoints, trimmed, de-duplicated and sorted.
func Sorted(endpoints []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		ep = strings.TrimSpace(ep)
		if ep == "" || seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	sort.Strings(out)
	return out
}
