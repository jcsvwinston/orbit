package admin

import (
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// ConsumeEventBus wires the live SQL and HTTP feeds to the framework's
// first-party EventBus (nucleus.Runtime.Observability()) — the orbit
// counterpart to ConsumeObservability, which takes the experimental
// *observability.Bus directly (SQL only). It subscribes to SQL and HTTP
// events, drains them into the live ring buffers + stream, and returns a stop
// function (also invoked from Close) that cancels both subscriptions.
//
// The HTTP lane is what makes host-application traffic reach the in-process
// panel: the framework's app-level HTTP middleware emits an HTTPEvent for
// every request, so the panel no longer depends on the host mounting
// LiveTrafficMiddleware (an internal type the module API cannot expose).
// Events whose path falls under the admin prefix are skipped here — the
// panel's own middleware lane records those (and only those), so a request is
// never recorded twice; live_exclude_patterns apply the same way they do in
// the middleware and the cluster relay ingest.
//
// The EventBus hands back detached value events and owns the underlying bus's
// pooled-event Release discipline internally, so these drains are plain (no
// Release, no buffered-event drain on cancel — cancel() closes the channels).
// Safe no-op when p, p.live, or eb is nil.
func (p *Panel) ConsumeEventBus(eb nucleus.EventBus) func() {
	if p == nil || p.live == nil || eb == nil {
		return func() {}
	}
	sqlCh, cancelSQL := eb.SubscribeSQL()
	httpCh, cancelHTTP := eb.SubscribeHTTP()
	p.observConnected.Store(true)

	done := make(chan struct{})
	stop := func() {
		p.observStopOnce.Do(func() {
			cancelSQL()
			cancelHTTP()
			close(done)
		})
	}
	p.observCancel = stop

	go func() {
		for {
			select {
			case ev, ok := <-sqlCh:
				if !ok {
					return
				}
				p.recordEventBusSQL(ev)
			case <-done:
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case ev, ok := <-httpCh:
				if !ok {
					return
				}
				p.recordEventBusHTTP(ev)
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

// recordEventBusHTTP converts a first-party nucleus.HTTPEvent (already
// sanitised/truncated by the framework's HTTP hook; PayloadPreview is the
// hook's redacted summary, never a raw body) into the live view's shape and
// funnels it into the shared pushLiveRequest feed.
//
// Two classes of events are skipped at write time:
//   - paths under the admin prefix — the panel's own liveTrafficMiddleware is
//     the single writer for the panel surface (it additionally records session
//     activity, which the bus event does not carry), so recording them here
//     would duplicate that lane;
//   - paths matching live_exclude_patterns — parity with the middleware and
//     the cluster relay ingest, and it keeps noise out of the ring buffer.
func (p *Panel) recordEventBusHTTP(e nucleus.HTTPEvent) {
	if p == nil || p.live == nil {
		return
	}
	path := strings.TrimSpace(e.Path)
	if p.adminPrefixOwnsPath(path) {
		return
	}
	if shouldExcludeLivePath(path, p.liveExcludePatterns()) {
		return
	}
	p.pushLiveRequest(liveRequestEvent{
		NodeID:         strings.TrimSpace(e.NodeID),
		Timestamp:      e.EmittedAt.UTC().Format(time.RFC3339),
		Method:         strings.TrimSpace(e.Method),
		Path:           truncateText(path, 240),
		Status:         e.Status,
		DurationMS:     e.Duration.Milliseconds(),
		RequestID:      strings.TrimSpace(e.RequestID),
		TraceID:        strings.TrimSpace(e.TraceID),
		UserID:         strings.TrimSpace(e.UserID),
		RemoteIP:       strings.TrimSpace(e.RemoteIP),
		UserAgent:      truncateText(strings.TrimSpace(e.UserAgent), 320),
		PayloadPreview: truncateText(strings.TrimSpace(e.PayloadPreview), 220),
	})
}

// adminPrefixOwnsPath reports whether path lives under the panel's mount
// prefix (normalized at NewPanel), i.e. whether the panel's own middleware —
// not the EventBus lane — is responsible for observing it.
func (p *Panel) adminPrefixOwnsPath(path string) bool {
	if p == nil {
		return false
	}
	prefix := strings.TrimSpace(p.config.Prefix)
	if prefix == "" {
		prefix = DefaultPrefix
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
