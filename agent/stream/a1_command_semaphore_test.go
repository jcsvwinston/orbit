// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// F17: server-driven commands run under a semaphore; a burst never has
// more than maxConcurrentCommands in flight and none is lost.
func TestRunCommand_BoundsConcurrency(t *testing.T) {
	s := New(nil, Config{})
	s.streamCtx, s.streamCancel = context.WithCancel(context.Background())
	defer s.streamCancel()

	const burst = maxConcurrentCommands * 3
	var inFlight, peak, ran atomic.Int32
	var wg sync.WaitGroup
	wg.Add(burst)
	for i := 0; i < burst; i++ {
		s.runCommand(func() {
			defer wg.Done()
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(3 * time.Millisecond)
			inFlight.Add(-1)
			ran.Add(1)
		})
	}
	wg.Wait()
	if int(ran.Load()) != burst {
		t.Fatalf("%d commands ran, want %d", ran.Load(), burst)
	}
	if peak.Load() > maxConcurrentCommands {
		t.Fatalf("%d commands in flight at once, cap is %d", peak.Load(), maxConcurrentCommands)
	}
	_ = adminv1.Frame{}
}
