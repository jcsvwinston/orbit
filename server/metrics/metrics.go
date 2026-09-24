// Package metrics holds the admin server's own Prometheus collectors: what
// the server does, beside what the Go runtime does. Each server owns one
// set, registered on its own registry and served on the metrics listener
// next to the default registry, so several servers in one process (tests)
// never collide on a name.
package metrics

import "github.com/prometheus/client_golang/prometheus"

// Counters are the server's monotonic collectors, bumped on the hot path.
type Counters struct {
	FramesReceivedTotal     prometheus.Counter
	EventsReceivedTotal     *prometheus.CounterVec // labels: type
	HeartbeatsReceivedTotal prometheus.Counter
	DataStudioRequestsTotal *prometheus.CounterVec // labels: outcome (ok, error)
	AlertsFiredTotal        prometheus.Counter
	AlertsResolvedTotal     prometheus.Counter
}

// NewCounters builds and registers the counters on reg.
func NewCounters(reg prometheus.Registerer) *Counters {
	c := &Counters{
		FramesReceivedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_server_frames_received_total",
			Help: "Frames received from agents on the bidi stream.",
		}),
		EventsReceivedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "admin_server_events_received_total",
			Help: "Events received from agents, by event type.",
		}, []string{"type"}),
		HeartbeatsReceivedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_server_heartbeats_received_total",
			Help: "Heartbeats received from agents.",
		}),
		DataStudioRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "admin_server_datastudio_requests_total",
			Help: "Data Studio requests routed to agents, by outcome.",
		}, []string{"outcome"}),
		AlertsFiredTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_server_alerts_fired_total",
			Help: "Alerts that fired.",
		}),
		AlertsResolvedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_server_alerts_resolved_total",
			Help: "Alerts that resolved.",
		}),
	}
	if reg != nil {
		reg.MustRegister(c.FramesReceivedTotal, c.EventsReceivedTotal, c.HeartbeatsReceivedTotal, c.DataStudioRequestsTotal, c.AlertsFiredTotal, c.AlertsResolvedTotal)
	}
	return c
}

// Derived are the collectors read from the server's state at scrape time:
// gauges for what is, counters for what the state already counts.
type Derived struct {
	NodesConnected       func() float64
	NodesKnown           func() float64
	AlertsFiring         func() float64
	ReplayBuffered       func() float64
	EventsPublishedTotal func() float64
	EventsDroppedTotal   func() float64
}

// RegisterDerived registers every non-nil function.
func RegisterDerived(reg prometheus.Registerer, d Derived) {
	if reg == nil {
		return
	}
	gauge := func(name, help string, f func() float64) {
		if f != nil {
			reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, f))
		}
	}
	counter := func(name, help string, f func() float64) {
		if f != nil {
			reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help}, f))
		}
	}
	gauge("admin_server_nodes_connected", "Agents with an open stream.", d.NodesConnected)
	gauge("admin_server_nodes_known", "Nodes in the registry, connected or not.", d.NodesKnown)
	gauge("admin_server_alerts_firing", "Alerts firing now.", d.AlertsFiring)
	gauge("admin_server_replay_buffered_events", "Events held in the replay buffers, all kinds.", d.ReplayBuffered)
	counter("admin_server_events_published_total", "Events fanned out to UI subscriptions.", d.EventsPublishedTotal)
	counter("admin_server_events_dropped_total", "Events a UI subscription could not take in time.", d.EventsDroppedTotal)
}
