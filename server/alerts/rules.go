// Package alerts evaluates threshold rules on the host metrics agents
// report, raises and resolves alerts, and notifies channels. Rules and
// channels are the server's configuration; the wire reads them
// (AlertService) and never writes them.
package alerts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// Duration is a time.Duration that reads from JSON as "30s" or as
// nanoseconds, so a rules file can say what a person says.
type Duration time.Duration

// UnmarshalJSON accepts a duration string or a number of nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("alerts: duration %q: %w", s, err)
		}
		*d = Duration(v)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("alerts: duration %s: want \"30s\" or nanoseconds", string(b))
	}
	*d = Duration(n)
	return nil
}

// MarshalJSON writes the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Rule is one threshold: fire when `metric op threshold` has held for
// `for` on a node the rule applies to; resolve when it stops holding.
type Rule struct {
	// ID is what alerts refer to; defaults to the name in lower case with
	// spaces as dashes.
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	// Metric is a HostMetrics field: cpu_percent, rss_bytes,
	// heap_alloc_bytes, goroutines, gc_pause_p99_ms, db_in_use, db_idle,
	// db_max_open.
	Metric string `json:"metric"`
	// Op is one of ">", ">=", "<", "<=", "==".
	Op        string  `json:"op"`
	Threshold float64 `json:"threshold"`
	// For is how long the condition must hold before the alert fires; zero
	// fires on the first breaching sample.
	For Duration `json:"for,omitempty"`
	// Severity is info, warning or critical (default warning).
	Severity string `json:"severity,omitempty"`
	// NodeIDs restricts the rule to these node ids, or glob patterns
	// (path.Match); empty applies to every node.
	NodeIDs []string `json:"node_ids,omitempty"`
	// Channels names the channels to notify when the alert fires and when
	// it resolves; empty notifies nobody (the alert is still raised).
	Channels []string `json:"channels,omitempty"`
}

// Metrics lists the HostMetrics fields a rule may name.
var Metrics = []string{"cpu_percent", "rss_bytes", "heap_alloc_bytes", "goroutines", "gc_pause_p99_ms", "db_in_use", "db_idle", "db_max_open"}

var ops = map[string]func(v, t float64) bool{
	">":  func(v, t float64) bool { return v > t },
	">=": func(v, t float64) bool { return v >= t },
	"<":  func(v, t float64) bool { return v < t },
	"<=": func(v, t float64) bool { return v <= t },
	"==": func(v, t float64) bool { return v == t },
}

// Severities are the accepted severity names.
var Severities = []string{"info", "warning", "critical"}

// Normalize fills the defaults (id, severity) and validates the rule.
func (r *Rule) Normalize() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("alerts: a rule needs a name")
	}
	if strings.TrimSpace(r.ID) == "" {
		r.ID = strings.ReplaceAll(strings.ToLower(r.Name), " ", "-")
	}
	r.Metric = strings.ToLower(strings.TrimSpace(r.Metric))
	if _, ok := metricValue(&adminv1.HostMetrics{}, r.Metric); !ok {
		return fmt.Errorf("alerts: rule %q: unknown metric %q (one of %s)", r.Name, r.Metric, strings.Join(Metrics, ", "))
	}
	r.Op = strings.TrimSpace(r.Op)
	if _, ok := ops[r.Op]; !ok {
		return fmt.Errorf("alerts: rule %q: unknown operator %q (one of >, >=, <, <=, ==)", r.Name, r.Op)
	}
	if r.For < 0 {
		return fmt.Errorf("alerts: rule %q: negative for", r.Name)
	}
	r.Severity = strings.ToLower(strings.TrimSpace(r.Severity))
	if r.Severity == "" {
		r.Severity = "warning"
	}
	okSev := false
	for _, s := range Severities {
		if s == r.Severity {
			okSev = true
		}
	}
	if !okSev {
		return fmt.Errorf("alerts: rule %q: unknown severity %q (one of %s)", r.Name, r.Severity, strings.Join(Severities, ", "))
	}
	for _, p := range r.NodeIDs {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("alerts: rule %q: node pattern %q: %w", r.Name, p, err)
		}
	}
	return nil
}

// appliesTo reports whether the rule covers the node.
func (r Rule) appliesTo(nodeID string) bool {
	if len(r.NodeIDs) == 0 {
		return true
	}
	for _, p := range r.NodeIDs {
		if ok, _ := path.Match(p, nodeID); ok || p == nodeID {
			return true
		}
	}
	return false
}

// breached reports whether the sample breaches the rule, and the value
// the rule looked at.
func (r Rule) breached(m *adminv1.HostMetrics) (float64, bool) {
	v, ok := metricValue(m, r.Metric)
	if !ok {
		return 0, false
	}
	return v, ops[r.Op](v, r.Threshold)
}

func metricValue(m *adminv1.HostMetrics, name string) (float64, bool) {
	switch name {
	case "cpu_percent":
		return m.GetCpuPercent(), true
	case "rss_bytes":
		return float64(m.GetRssBytes()), true
	case "heap_alloc_bytes":
		return float64(m.GetHeapAllocBytes()), true
	case "goroutines":
		return float64(m.GetGoroutines()), true
	case "gc_pause_p99_ms":
		return m.GetGcPauseP99Ms(), true
	case "db_in_use":
		return float64(m.GetDbInUse()), true
	case "db_idle":
		return float64(m.GetDbIdle()), true
	case "db_max_open":
		return float64(m.GetDbMaxOpen()), true
	}
	return 0, false
}

// ParseRules reads a rules file: a JSON array of rules, or an object with
// a "rules" array. Every rule is normalized; the first invalid one is the
// error.
func ParseRules(r io.Reader) ([]Rule, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("alerts: read rules: %w", err)
	}
	var rules []Rule
	if err := json.Unmarshal(raw, &rules); err != nil {
		var wrapped struct {
			Rules []Rule `json:"rules"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			return nil, fmt.Errorf("alerts: rules file: want a JSON array of rules or {\"rules\": [...]}: %w", err)
		}
		rules = wrapped.Rules
	}
	seen := map[string]bool{}
	for i := range rules {
		if err := rules[i].Normalize(); err != nil {
			return nil, err
		}
		if seen[rules[i].ID] {
			return nil, fmt.Errorf("alerts: two rules share the id %q", rules[i].ID)
		}
		seen[rules[i].ID] = true
	}
	return rules, nil
}
