package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// State is an alert's state.
type State int

const (
	Firing State = iota + 1
	Resolved
)

// String names the state.
func (s State) String() string {
	switch s {
	case Firing:
		return "firing"
	case Resolved:
		return "resolved"
	}
	return "unspecified"
}

// Alert is one rule firing on one node, and its resolution.
type Alert struct {
	ID         string
	RuleID     string
	NodeID     string
	State      State
	Value      float64
	FiredAt    time.Time
	ResolvedAt time.Time
	Message    string
}

// Channel delivers an alert somewhere. Notify is called for the firing
// and for the resolution; it must be safe to call concurrently.
type Channel interface {
	Name() string
	Notify(ctx context.Context, a Alert, r Rule) error
}

// historyLimit bounds the resolved alerts the engine remembers.
const historyLimit = 1024

// Engine evaluates rules against the samples it observes.
type Engine struct {
	rules    []Rule
	channels map[string]Channel
	logger   *slog.Logger
	now      func() time.Time

	mu      sync.Mutex
	pending map[string]pendingBreach // rule/node → since when the condition holds
	firing  map[string]*Alert        // rule/node → the alert
	history []Alert                  // resolved, oldest first, bounded
	subs    map[int]chan Alert
	nextSub int

	notifications chan notification
	notifyTimeout time.Duration
}

type pendingBreach struct {
	since time.Time
	value float64
}

type notification struct {
	alert Alert
	rule  Rule
}

// New builds an engine over normalized rules and channels. A rule that
// names a channel nobody configured is an error: a rule that would notify
// nobody by mistake is the quiet failure alerts exist to prevent.
func New(rules []Rule, channels []Channel, logger *slog.Logger) (*Engine, error) {
	if logger == nil {
		logger = slog.Default()
	}
	e := &Engine{
		rules:         make([]Rule, 0, len(rules)),
		channels:      make(map[string]Channel, len(channels)),
		logger:        logger,
		now:           time.Now,
		pending:       map[string]pendingBreach{},
		firing:        map[string]*Alert{},
		subs:          map[int]chan Alert{},
		notifications: make(chan notification, 256),
		notifyTimeout: 10 * time.Second,
	}
	for _, c := range channels {
		if c == nil {
			continue
		}
		if _, dup := e.channels[c.Name()]; dup {
			return nil, fmt.Errorf("alerts: two channels named %q", c.Name())
		}
		e.channels[c.Name()] = c
	}
	for _, r := range rules {
		if err := r.Normalize(); err != nil {
			return nil, err
		}
		for _, name := range r.Channels {
			if _, ok := e.channels[name]; !ok {
				return nil, fmt.Errorf("alerts: rule %q names channel %q, which is not configured", r.Name, name)
			}
		}
		e.rules = append(e.rules, r)
	}
	return e, nil
}

// Rules returns the rules the engine evaluates.
func (e *Engine) Rules() []Rule {
	if e == nil {
		return nil
	}
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// ChannelNames lists the configured channels.
func (e *Engine) ChannelNames() []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.channels))
	for n := range e.channels {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Observe evaluates one node's sample against every rule that applies and
// returns the alerts that changed state because of it (fired or
// resolved). Safe to call concurrently.
func (e *Engine) Observe(nodeID string, m *adminv1.HostMetrics, at time.Time) []Alert {
	if e == nil || m == nil {
		return nil
	}
	if at.IsZero() {
		at = e.now()
	}
	var changed []Alert
	e.mu.Lock()
	for _, r := range e.rules {
		if !r.appliesTo(nodeID) {
			continue
		}
		key := r.ID + "\x00" + nodeID
		value, breached := r.breached(m)
		switch {
		case breached && e.firing[key] != nil:
			e.firing[key].Value = value
		case breached:
			p, holding := e.pending[key]
			if !holding {
				p = pendingBreach{since: at}
				e.pending[key] = p
			}
			if at.Sub(p.since) >= time.Duration(r.For) {
				delete(e.pending, key)
				a := &Alert{
					ID:      fmt.Sprintf("%s/%s/%d", r.ID, nodeID, at.UnixNano()),
					RuleID:  r.ID,
					NodeID:  nodeID,
					State:   Firing,
					Value:   value,
					FiredAt: at,
					Message: fmt.Sprintf("%s: %s %s %g on %s (value %g)", r.Name, r.Metric, r.Op, r.Threshold, nodeID, value),
				}
				e.firing[key] = a
				changed = append(changed, *a)
				e.emitLocked(*a, r)
			}
		default:
			delete(e.pending, key)
			if a := e.firing[key]; a != nil {
				delete(e.firing, key)
				a.State = Resolved
				a.ResolvedAt = at
				a.Value = value
				e.history = append(e.history, *a)
				if len(e.history) > historyLimit {
					e.history = e.history[len(e.history)-historyLimit:]
				}
				changed = append(changed, *a)
				e.emitLocked(*a, r)
			}
		}
	}
	e.mu.Unlock()
	return changed
}

// emitLocked hands a state change to the subscribers and the notifier.
// Called with e.mu held.
func (e *Engine) emitLocked(a Alert, r Rule) {
	for _, ch := range e.subs {
		select {
		case ch <- a:
		default: // a slow subscriber misses a change rather than blocking evaluation
		}
	}
	if len(r.Channels) == 0 {
		return
	}
	select {
	case e.notifications <- notification{alert: a, rule: r}:
	default:
		e.logger.Warn("admin server: alert notification queue full; dropping", "alert", a.ID)
	}
}

// Alerts returns the alerts: firing first, newest first, then the resolved
// ones newest first when includeResolved. limit <= 0 returns all.
func (e *Engine) Alerts(includeResolved bool, limit int) []Alert {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	out := make([]Alert, 0, len(e.firing)+len(e.history))
	for _, a := range e.firing {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FiredAt.After(out[j].FiredAt) })
	if includeResolved {
		for i := len(e.history) - 1; i >= 0; i-- {
			out = append(out, e.history[i])
		}
	}
	e.mu.Unlock()
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// FiringCount is how many alerts are firing now.
func (e *Engine) FiringCount() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.firing)
}

// Subscribe returns a channel that receives every state change until
// cancel is called. A subscriber that does not keep up misses changes.
func (e *Engine) Subscribe(buffer int) (<-chan Alert, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Alert, buffer)
	e.mu.Lock()
	id := e.nextSub
	e.nextSub++
	e.subs[id] = ch
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, id)
		e.mu.Unlock()
	}
}

// Run delivers notifications to channels until ctx ends. One delivery at
// a time per engine, each bounded by a timeout; a channel that fails is
// logged, not retried — the alert itself is raised regardless.
func (e *Engine) Run(ctx context.Context) {
	if e == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-e.notifications:
			for _, name := range n.rule.Channels {
				c := e.channels[name]
				if c == nil {
					continue
				}
				cctx, cancel := context.WithTimeout(ctx, e.notifyTimeout)
				if err := c.Notify(cctx, n.alert, n.rule); err != nil {
					e.logger.Warn("admin server: alert notification failed", "channel", name, "alert", n.alert.ID, "error", err)
				}
				cancel()
			}
		}
	}
}
