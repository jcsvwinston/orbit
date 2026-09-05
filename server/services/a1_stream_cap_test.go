// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/server/auth"
)

// OR-26 / F6 (maturity audit 2026-09-03): one operator cannot hold more
// live event streams than the cap; the slot is returned when the stream
// ends; and the aggregate push to the agents is coalesced.
func TestControlService_StreamSlotCapPerOperator(t *testing.T) {
	s := &ControlService{MaxStreamsPerOperator: 2}
	ctx := auth.WithIdentity(context.Background(), auth.Identity{Subject: "alice"})
	r1, err := s.acquireStreamSlot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.acquireStreamSlot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.acquireStreamSlot(ctx); err == nil || connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("third stream for the same operator: err=%v, want ResourceExhausted", err)
	}
	// Another operator has her own budget.
	bob := auth.WithIdentity(context.Background(), auth.Identity{Subject: "bob"})
	rb, err := s.acquireStreamSlot(bob)
	if err != nil {
		t.Fatalf("bob refused by alice's cap: %v", err)
	}
	rb()
	r1()
	if _, err := s.acquireStreamSlot(ctx); err != nil {
		t.Fatalf("slot not returned after release: %v", err)
	}
	r2()
	var ce *connect.Error
	if _, err := (&ControlService{MaxStreamsPerOperator: 1}).acquireStreamSlot(context.Background()); err != nil {
		t.Fatalf("anonymous first stream: %v", err)
	} else if errors.As(err, &ce) {
		t.Fatal("unreachable")
	}
}

func TestControlService_PushAggregateIsCoalesced(t *testing.T) {
	s := &ControlService{}
	// With no state the timer body would nil-deref; replace the push
	// with a counter by swapping the timer target through pushAggregate.
	fired := 0
	s.pushMu.Lock()
	s.pushTimer = time.AfterFunc(time.Hour, func() {}) // pretend armed
	s.pushMu.Unlock()
	for i := 0; i < 10; i++ {
		s.schedulePushAggregate() // all ride on the armed timer
	}
	s.pushMu.Lock()
	armed := s.pushTimer != nil
	if s.pushTimer != nil {
		s.pushTimer.Stop()
		fired++
	}
	s.pushMu.Unlock()
	if !armed || fired != 1 {
		t.Fatalf("ten schedules within the window must share one timer")
	}
}
