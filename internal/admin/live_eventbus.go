package admin

import (
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// ConsumeEventBus wires the live SQL feed to the framework's first-party
// EventBus (nucleus.Runtime.Observability()) — the orbit counterpart to
// ConsumeObservability, which takes the experimental *observability.Bus
// directly. It subscribes to SQL events, drains them into the live ring buffer +
// stream, and returns a stop function (also invoked from Close).
//
// The EventBus hands back detached value events and owns the underlying bus's
// pooled-event Release discipline internally, so this drain is plain (no
// Release, no buffered-event drain on cancel — cancel() closes the channel).
// Safe no-op when p, p.live, or eb is nil.
func (p *Panel) ConsumeEventBus(eb nucleus.EventBus) func() {
	if p == nil || p.live == nil || eb == nil {
		return func() {}
	}
	ch, cancel := eb.SubscribeSQL()
	p.observConnected.Store(true)

	done := make(chan struct{})
	stop := func() {
		p.observStopOnce.Do(func() {
			cancel()
			close(done)
		})
	}
	p.observCancel = stop

	go func() {
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				p.recordEventBusSQL(ev)
			case <-done:
				return
			}
		}
	}()
	return stop
}

// recordEventBusSQL mirrors recordBusSQL but sourced from a first-party
// nucleus.SQLEvent value (already sanitised/truncated by the framework's SQL
// hook). It funnels into the shared pushLiveSQL feed.
func (p *Panel) recordEventBusSQL(e nucleus.SQLEvent) {
	if p == nil || p.live == nil {
		return
	}
	p.pushLiveSQL(liveSQLEvent{
		NodeID:     strings.TrimSpace(e.NodeID),
		Timestamp:  e.EmittedAt.UTC().Format(time.RFC3339),
		ModelName:  strings.TrimSpace(e.ModelName),
		Operation:  truncateText(strings.TrimSpace(e.Operation), 64),
		Query:      truncateText(compactSQL(e.Query), 640),
		Args:       append([]string(nil), e.Args...),
		DurationMS: e.Duration.Milliseconds(),
		Error:      truncateText(strings.TrimSpace(e.Err), 220),
		RequestID:  strings.TrimSpace(e.RequestID),
		TraceID:    strings.TrimSpace(e.TraceID),
		UserID:     strings.TrimSpace(e.UserID),
	})
}
