package agent

import (
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent/buffer"
	"github.com/jcsvwinston/orbit/agent/convert"
)

// catcher is the bus listener the agent runs while it has no stream: it
// subscribes under the filters the server had when the stream ended and
// parks the events in the per-kind ring buffer, which the next stream
// drains right after registering. Bounded like the buffer is: an outage
// longer than the buffer keeps the newest events per kind, and the drop
// counter says how many went.
//
// Before it, the agent subscribed to the bus only while a stream was
// open, so an event emitted between a disconnect and the next accepted
// stream was captured nowhere (buffer package doc, "does NOT bridge
// disconnects"). It bridges them now, within the buffer's capacity.
type catcher struct {
	cancels []func()
	stop    chan struct{} // Cancel does not close a subscription's channel; this ends the drains
	wg      sync.WaitGroup
}

// startCatcher begins catching under filters. No filters means the server
// wanted nothing: nothing is caught.
func (a *Agent) startCatcher(filters []observability.Filter) {
	if a.cfg.Bus == nil || a.bufs == nil || len(filters) == 0 {
		return
	}
	a.stopCatcher()
	c := &catcher{stop: make(chan struct{})}
	for _, f := range filters {
		sub, cancel := a.cfg.Bus.Subscribe(f, &observability.SubscribeOptions{ChannelSize: 256})
		c.cancels = append(c.cancels, cancel)
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.stop:
					return
				case ev, ok := <-sub.Ch():
					if !ok {
						return
					}
					a.park(ev)
				}
			}
		}()
	}
	a.catcher = c
	a.cfg.Logger.Debug("admin agent parks events until the next stream", "filters", len(filters), "node_id", a.nodeID)
}

// park converts one bus event and pushes it into the ring buffer.
func (a *Agent) park(ev observability.Event) {
	defer ev.Release()
	pb := convert.EventToProto(ev)
	if pb == nil {
		return
	}
	pb.NodeId = a.nodeID
	var buf *buffer.Buffer
	if buf = a.bufs.For(ev.Kind()); buf == nil {
		return
	}
	kind := ev.Kind().String()
	if buf.Push(pb) {
		a.metrics.EventsDroppedTotal.WithLabelValues(kind, "buffer_full").Inc()
	}
	a.metrics.EventsParkedTotal.WithLabelValues(kind).Inc()
}

// stopCatcher ends the catcher, if one runs, and waits for its goroutines.
func (a *Agent) stopCatcher() {
	c := a.catcher
	if c == nil {
		return
	}
	a.catcher = nil
	for _, cancel := range c.cancels {
		cancel()
	}
	close(c.stop)
	c.wg.Wait()
}
