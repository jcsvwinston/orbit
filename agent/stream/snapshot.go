package stream

import (
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// handleSnapshotRequest answers a server-routed SnapshotRequest with the
// data the agent can produce from inside the process. Two providers are
// built in — GO_RUNTIME and REGISTERED_MODELS; the remaining snapshot
// types need app-level data the agent has no hook for yet, and answer
// with an explicit per-type error instead of a blanket "not implemented".
//
// Runs in its own goroutine: ReadMemStats stops the world briefly and
// must never block recvLoop.
func (s *Stream) handleSnapshotRequest(req *adminv1.SnapshotRequest) {
	if req == nil {
		return
	}
	go func() {
		resp := &adminv1.SnapshotResponse{
			RequestId: req.GetRequestId(),
			Type:      req.GetType(),
		}
		payload, err := s.snapshotPayload(req.GetType())
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.PayloadJson = payload
		}
		s.queueFrame(&adminv1.Frame{
			Body: &adminv1.Frame_SnapshotResponse{SnapshotResponse: resp},
		})
	}()
}

func (s *Stream) snapshotPayload(t adminv1.SnapshotType) ([]byte, error) {
	switch t {
	case adminv1.SnapshotType_SNAPSHOT_TYPE_GO_RUNTIME:
		return s.goRuntimeSnapshot()
	case adminv1.SnapshotType_SNAPSHOT_TYPE_REGISTERED_MODELS:
		return s.registeredModelsSnapshot()
	default:
		return nil, fmt.Errorf("admin agent: no snapshot provider for %s on this node", t.String())
	}
}

// goRuntimeSnapshot reports the Go runtime facts every process can
// answer: version, scheduler and heap numbers, GC counters, uptime.
func (s *Stream) goRuntimeSnapshot() ([]byte, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	payload := map[string]any{
		"node_id":          s.cfg.NodeID,
		"agent_version":    s.cfg.Version,
		"go_version":       runtime.Version(),
		"goroutines":       runtime.NumGoroutine(),
		"gomaxprocs":       runtime.GOMAXPROCS(0),
		"num_cpu":          runtime.NumCPU(),
		"heap_alloc_bytes": m.HeapAlloc,
		"heap_sys_bytes":   m.HeapSys,
		"heap_objects":     m.HeapObjects,
		"gc_runs":          m.NumGC,
		"gc_pause_total":   time.Duration(m.PauseTotalNs).String(),
		"started_at":       s.cfg.StartedAt.UTC().Format(time.RFC3339),
		"uptime":           time.Since(s.cfg.StartedAt).Round(time.Second).String(),
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("admin agent: marshal go_runtime snapshot: %w", err)
	}
	return out, nil
}

// registeredModelsSnapshot lists the Data Studio models this node
// registered — the same set NodeRegistration carries at connect time,
// but queryable on demand.
func (s *Stream) registeredModelsSnapshot() ([]byte, error) {
	if s.cfg.DataStudio == nil {
		return nil, fmt.Errorf("admin agent: data studio is not enabled on this node")
	}
	models := s.cfg.DataStudio.RegisteredModels()
	if models == nil {
		models = []string{}
	}
	payload := map[string]any{
		"node_id": s.cfg.NodeID,
		"models":  models,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("admin agent: marshal registered_models snapshot: %w", err)
	}
	return out, nil
}
