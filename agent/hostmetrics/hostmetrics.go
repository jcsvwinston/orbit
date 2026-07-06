// Package hostmetrics samples the agent process's runtime health for the
// Heartbeat frame: CPU share, memory, goroutines, GC pauses, and the
// framework database pool. Standard library only; fields a platform cannot
// report ship as zero and the UI renders them as absent.
package hostmetrics

import (
	"database/sql"
	"runtime"
	"sort"
	"sync"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// Sampler produces HostMetrics samples. Safe for use from the heartbeat
// goroutine; CPU deltas are tracked between consecutive Collect calls.
type Sampler struct {
	mu       sync.Mutex
	db       *sql.DB // optional; nil when the app has no managed database
	prevCPU  time.Duration
	prevWall time.Time
}

// New returns a Sampler. db may be nil.
func New(db *sql.DB) *Sampler {
	return &Sampler{db: db}
}

// Collect returns a point-in-time sample.
func (s *Sampler) Collect() *adminv1.HostMetrics {
	if s == nil {
		return nil
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	m := &adminv1.HostMetrics{
		HeapAllocBytes: ms.HeapAlloc,
		Goroutines:     uint32(runtime.NumGoroutine()),
		GcPauseP99Ms:   gcPauseP99Ms(&ms),
		RssBytes:       readRSS(),
	}

	s.mu.Lock()
	now := time.Now()
	if cpu, ok := cpuTime(); ok {
		if !s.prevWall.IsZero() {
			wall := now.Sub(s.prevWall)
			if wall > 0 {
				pct := float64(cpu-s.prevCPU) / float64(wall) * 100
				if pct < 0 {
					pct = 0
				}
				m.CpuPercent = pct
			}
		}
		s.prevCPU = cpu
		s.prevWall = now
	}
	db := s.db
	s.mu.Unlock()

	if db != nil {
		st := db.Stats()
		m.DbInUse = uint32(st.InUse)
		m.DbIdle = uint32(st.Idle)
		m.DbMaxOpen = uint32(st.MaxOpenConnections)
	}
	return m
}

// gcPauseP99Ms computes the p99 of the runtime's recent GC pause ring (up to
// 256 samples). Returns 0 before the first GC.
func gcPauseP99Ms(ms *runtime.MemStats) float64 {
	n := int(ms.NumGC)
	if n == 0 {
		return 0
	}
	if n > len(ms.PauseNs) {
		n = len(ms.PauseNs)
	}
	pauses := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		if p := ms.PauseNs[i]; p > 0 {
			pauses = append(pauses, p)
		}
	}
	if len(pauses) == 0 {
		return 0
	}
	sort.Slice(pauses, func(i, j int) bool { return pauses[i] < pauses[j] })
	idx := (len(pauses)*99 + 99) / 100
	if idx > 0 {
		idx--
	}
	return float64(pauses[idx]) / 1e6
}
