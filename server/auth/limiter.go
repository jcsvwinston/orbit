package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// failureLimiter is a small fixed-window lockout for failed credential
// presentations, keyed by remote IP. It only counts requests that
// PRESENTED a wrong credential (bad bearer, bad proxy secret with a
// bearer attempt) — credential-less 401s are not brute force and are
// never counted, so an unauthenticated browser hitting the SPA cannot
// lock anyone out.
//
// Deliberately modest: it exists to make online brute force of the
// shared tokens impractical, not to be a general WAF. Constants rather
// than knobs; the canonical production deployment authenticates at the
// reverse proxy anyway.
type failureLimiter struct {
	mu      sync.Mutex
	windows map[string]*failWindow
}

type failWindow struct {
	count   int
	resetAt time.Time
}

const (
	// failureLimit failures within failureWindow lock the IP out until
	// the window expires.
	failureLimit  = 20
	failureWindow = time.Minute
	// failureMapCap bounds the tracking map so an attacker rotating
	// source addresses cannot grow it unboundedly; when full, expired
	// windows are pruned and — as a last resort — new IPs go untracked
	// (fail-open on tracking, never on auth itself).
	failureMapCap = 4096
)

func newFailureLimiter() *failureLimiter {
	return &failureLimiter{windows: make(map[string]*failWindow)}
}

// blocked reports whether ip is currently locked out.
func (l *failureLimiter) blocked(ip string) bool {
	if l == nil || ip == "" {
		return false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[ip]
	if !ok {
		return false
	}
	if now.After(w.resetAt) {
		delete(l.windows, ip)
		return false
	}
	return w.count >= failureLimit
}

// fail records one failed credential presentation for ip.
func (l *failureLimiter) fail(ip string) {
	if l == nil || ip == "" {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if w, ok := l.windows[ip]; ok && now.Before(w.resetAt) {
		w.count++
		return
	}
	if len(l.windows) >= failureMapCap {
		for k, w := range l.windows {
			if now.After(w.resetAt) {
				delete(l.windows, k)
			}
		}
		if len(l.windows) >= failureMapCap {
			return
		}
	}
	l.windows[ip] = &failWindow{count: 1, resetAt: now.Add(failureWindow)}
}

// remoteIPString returns the bare remote IP for limiter keying ("" when
// unparseable — those requests are never limited).
func remoteIPString(r *http.Request) string {
	ip := remoteIP(r)
	if ip == nil {
		return ""
	}
	return strings.TrimSpace(ip.String())
}
