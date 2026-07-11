package routing

import (
	"sync"
	"time"
)

// AuditEntry is one fleet-plane action: something an operator did
// THROUGH the admin server (Data Studio mutations, and future manage
// actions). Per-app admin actions stay in each node's in-process Orbit
// audit ring; this ring covers the fleet plane the server itself routes.
type AuditEntry struct {
	Time   time.Time
	Actor  string
	Action string // e.g. "datastudio.create"
	Target string // human-readable, e.g. `Article #42 ("default")`
	NodeID string
}

// AuditRing is a bounded, in-memory, newest-wins ring of fleet-plane
// audit entries. Same discipline as event replay: never persisted,
// drop-oldest on overflow.
type AuditRing struct {
	mu      sync.Mutex
	entries []AuditEntry // circular; next is the write cursor
	next    int
	full    bool
}

// NewAuditRing constructs a ring. capacity <= 0 defaults to 2048.
func NewAuditRing(capacity int) *AuditRing {
	if capacity <= 0 {
		capacity = 2048
	}
	return &AuditRing{entries: make([]AuditEntry, capacity)}
}

// Append records one entry, evicting the oldest when full.
func (r *AuditRing) Append(e AuditEntry) {
	if r == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	r.mu.Lock()
	r.entries[r.next] = e
	r.next++
	if r.next == len(r.entries) {
		r.next = 0
		r.full = true
	}
	r.mu.Unlock()
}

// List returns up to limit entries, newest first. limit <= 0 means all.
func (r *AuditRing) List(limit int) []AuditEntry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	n := r.next
	if r.full {
		n = len(r.entries)
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]AuditEntry, 0, limit)
	// Walk backwards from the newest entry.
	idx := r.next - 1
	for len(out) < limit {
		if idx < 0 {
			idx = len(r.entries) - 1
		}
		out = append(out, r.entries[idx])
		idx--
	}
	return out
}

// Len is intended for tests.
func (r *AuditRing) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return len(r.entries)
	}
	return r.next
}
