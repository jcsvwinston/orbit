package routing

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// ErrRbacTimeout is returned when an agent does not answer an
// RbacRequest within the configured timeout.
var ErrRbacTimeout = errors.New("admin server: rbac request timed out")

// ErrRbacInflightOverflow is returned when too many simultaneous rbac
// requests are pending.
var ErrRbacInflightOverflow = errors.New("admin server: too many in-flight rbac requests")

// RbacRouter correlates server-issued RbacRequest frames with the
// RbacResponse frames the agent sends back over the same bidi stream.
// Mirrors the DataStudioRouter/SnapshotRouter pattern.
type RbacRouter struct {
	mu       sync.Mutex
	pending  map[string]chan *adminv1.RbacResponse
	maxAlive int
	nextID   atomic.Uint64
}

// NewRbacRouter constructs a router. maxAlive caps the total number of
// pending requests across all agents; values <= 0 default to 64.
func NewRbacRouter(maxAlive int) *RbacRouter {
	if maxAlive <= 0 {
		maxAlive = 64
	}
	return &RbacRouter{
		pending:  make(map[string]chan *adminv1.RbacResponse),
		maxAlive: maxAlive,
	}
}

// Begin allocates a fresh request_id and registers a channel. The
// returned cancel MUST be called even on success to release the slot.
func (r *RbacRouter) Begin() (id string, ch chan *adminv1.RbacResponse, cancel func(), err error) {
	r.mu.Lock()
	if len(r.pending) >= r.maxAlive {
		r.mu.Unlock()
		return "", nil, nil, ErrRbacInflightOverflow
	}
	id = "rbac-" + strconv.FormatUint(r.nextID.Add(1), 10)
	ch = make(chan *adminv1.RbacResponse, 1)
	r.pending[id] = ch
	r.mu.Unlock()

	cancel = func() {
		r.mu.Lock()
		if existing, ok := r.pending[id]; ok && existing == ch {
			delete(r.pending, id)
		}
		r.mu.Unlock()
	}
	return id, ch, cancel, nil
}

// Resolve delivers an RbacResponse to the awaiting Begin caller (if
// any). Returns true when a pending request consumed the response.
func (r *RbacRouter) Resolve(resp *adminv1.RbacResponse) bool {
	if resp == nil || resp.RequestId == "" {
		return false
	}
	r.mu.Lock()
	ch, ok := r.pending[resp.RequestId]
	if ok {
		delete(r.pending, resp.RequestId)
	}
	r.mu.Unlock()

	if !ok {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

// PendingCount is intended for tests / metrics.
func (r *RbacRouter) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// WaitRbac blocks for a response on ch up to the deadline. Cancel is
// the cleanup func from Begin (always run, even on success).
func WaitRbac(ch <-chan *adminv1.RbacResponse, cancel func(), timeout time.Duration) (*adminv1.RbacResponse, error) {
	defer cancel()
	if timeout <= 0 {
		return <-ch, nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, ErrRbacTimeout
	}
}
