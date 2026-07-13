package routing

// Tests for the server-side sampling leg (OR-FLEET-1): the per-sub
// sampling_rate used to be stored and never applied, and the aggregate
// Subscribe never carried rates. Deterministic cases only (rates 0 and
// 1) — no probabilistic assertions.

import (
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// (httpEvent lives in eventbus_test.go.)

func httpFilter() *adminv1.Filter {
	return &adminv1.Filter{Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST}}
}

func sampleEvent() *adminv1.Event { return httpEvent("node-a", "GET", "/x", 200) }

func TestEventBus_SamplingRateZero_DropsAll(t *testing.T) {
	b := NewEventBus()
	sub, cancel := b.Subscribe(httpFilter(), map[string]float32{"HTTP_REQUEST": 0}, 8)
	defer cancel()

	if got := b.Publish(sampleEvent()); got != 0 {
		t.Fatalf("delivered = %d, want 0 (rate 0)", got)
	}
	select {
	case ev := <-sub.Ch():
		t.Fatalf("unexpected delivery: %+v", ev)
	default:
	}
	if s := b.Stats(); s.Sampled != 1 {
		t.Errorf("Stats.Sampled = %d, want 1", s.Sampled)
	}
}

func TestEventBus_SamplingResidual_PerSubscription(t *testing.T) {
	b := NewEventBus()
	full, cancelFull := b.Subscribe(httpFilter(), nil, 8)
	defer cancelFull()
	zero, cancelZero := b.Subscribe(httpFilter(), map[string]float32{"HTTP_REQUEST": 0}, 8)
	defer cancelZero()

	// The aggregate ships at max(1, 0) = 1; the full sub keeps 1/1,
	// the zero sub keeps 0/1.
	if got := b.Publish(sampleEvent()); got != 1 {
		t.Fatalf("delivered = %d, want 1 (full sub only)", got)
	}
	select {
	case <-full.Ch():
	default:
		t.Error("full-rate subscription did not receive the event")
	}
	select {
	case ev := <-zero.Ch():
		t.Errorf("zero-rate subscription received %+v", ev)
	default:
	}
}

func TestEventBus_AggregateSampling_MaxAcrossSubs(t *testing.T) {
	b := NewEventBus()

	// No subscribers → nil.
	if got := b.AggregateSampling(); got != nil {
		t.Fatalf("empty bus aggregate sampling = %v, want nil", got)
	}

	_, cancelA := b.Subscribe(httpFilter(), map[string]float32{"HTTP_REQUEST": 0.2}, 8)
	defer cancelA()
	_, cancelB := b.Subscribe(httpFilter(), map[string]float32{"HTTP_REQUEST": 0.5}, 8)
	defer cancelB()

	agg := b.AggregateSampling()
	if agg == nil || agg["HTTP_REQUEST"] != 0.5 {
		t.Fatalf("aggregate sampling = %v, want HTTP_REQUEST: 0.5 (max)", agg)
	}

	// A sub without an entry demands 1.0 → the kind is omitted (proto
	// default already means "ship everything").
	_, cancelC := b.Subscribe(httpFilter(), nil, 8)
	defer cancelC()
	if agg := b.AggregateSampling(); agg != nil {
		t.Fatalf("aggregate sampling with a full-rate sub = %v, want nil", agg)
	}
}

func TestNormalizeRates_KeysAndClamping(t *testing.T) {
	got := normalizeRates(map[string]float32{
		"event_type_http_request": -0.5,
		" sql_statement ":         2.0,
	})
	if got["HTTP_REQUEST"] != 0 {
		t.Errorf("HTTP_REQUEST = %v, want clamped 0", got["HTTP_REQUEST"])
	}
	if got["SQL_STATEMENT"] != 1 {
		t.Errorf("SQL_STATEMENT = %v, want clamped 1", got["SQL_STATEMENT"])
	}
}
