package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// recordingStore is a storage.Store that answers List() empty and records
// the Prefix it was asked for. Every other method is an inert stub — the
// browse handler only calls List.
type recordingStore struct {
	mu         sync.Mutex
	lastPrefix string
	called     bool
}

func (s *recordingStore) List(_ context.Context, opts storage.ListOptions) (storage.ListResult, error) {
	s.mu.Lock()
	s.lastPrefix = opts.Prefix
	s.called = true
	s.mu.Unlock()
	return storage.ListResult{}, nil
}

func (s *recordingStore) Put(context.Context, string, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
func (s *recordingStore) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return nil, storage.ObjectInfo{}, nil
}
func (s *recordingStore) Delete(context.Context, string) error         { return nil }
func (s *recordingStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (s *recordingStore) PublicURL(context.Context, string, storage.URLConfig) (string, error) {
	return "", nil
}
func (s *recordingStore) SignedURL(context.Context, string, time.Duration, storage.URLConfig) (string, error) {
	return "", nil
}
func (s *recordingStore) Copy(context.Context, string, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
func (s *recordingStore) Close() error { return nil }

// TestListStorageConfinesPathToRoot pins that the store-backed browse
// handler confines the caller-supplied path to adminStorageBrowseRoot,
// exactly like normalizeStorageBrowsePath already promises the panel does.
//
// normalizeStorageBrowsePath has twelve subtests asserting that "../etc"
// and "private" are denied — but it had ZERO production callers: the store
// branch of handleListStorage passed c.Query("path") straight into
// listConfiguredStorage → store.List(Prefix: path+"/"). So a session with
// storage_view could list any prefix of the bucket, "uploads/" confinement
// and traversal rejection alike ignored. The engine holds no line here; the
// handler was the only gate and it was open.
//
// The handler is driven directly (not through panel.Handler()) so the test
// exercises the seam in isolation: with Auth nil the mux routes /api/* to
// the SPA fallback, so an end-to-end GET never reaches this handler at all
// (that routing gap is a separate finding). authorizeAction returns nil
// under Auth nil, so the storage_view gate is satisfied without a session.
func TestListStorageConfinesPathToRoot(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	rec := &recordingStore{}
	panel.store = rec

	cases := []struct {
		name       string
		query      string
		wantDenied bool
	}{
		{"traversal is denied", "../etc", true},
		{"absolute escape is denied", "/etc/passwd", true},
		{"sibling prefix is denied", "private", true},
		{"empty confines to root", "", false},
		{"uploads subdir is allowed", "uploads/invoices", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec.mu.Lock()
			rec.lastPrefix = ""
			rec.called = false
			rec.mu.Unlock()

			req := httptest.NewRequest("GET", "/admin/api/storage?path="+tc.query, nil)
			w := httptest.NewRecorder()
			c := router.NewContext(w, req, nil)

			err := panel.handleListStorage(c)

			rec.mu.Lock()
			prefix := rec.lastPrefix
			called := rec.called
			rec.mu.Unlock()

			if tc.wantDenied {
				if err == nil && called && (prefix == "" || strings.HasPrefix(prefix, adminStorageBrowseRoot)) {
					// A denied path may legitimately confine to root instead
					// of erroring, but it must NOT reach the store outside it.
				}
				if err == nil && called && prefix != "" && !strings.HasPrefix(prefix, adminStorageBrowseRoot) {
					t.Errorf("path %q reached the store with prefix %q, escaping %q — it must be denied or confined",
						tc.query, prefix, adminStorageBrowseRoot)
				}
				if err == nil && w.Code == 200 && called && strings.Contains(prefix, "..") {
					t.Errorf("traversal path %q reached the store with prefix %q", tc.query, prefix)
				}
				return
			}

			if err != nil {
				t.Fatalf("path %q should be allowed, got error %v", tc.query, err)
			}
			var payload struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &payload)
			if called && !strings.HasPrefix(prefix, adminStorageBrowseRoot) {
				t.Errorf("path %q reached the store with prefix %q, escaping %q", tc.query, prefix, adminStorageBrowseRoot)
			}
			if payload.Path != "" && !strings.HasPrefix(payload.Path, adminStorageBrowseRoot) {
				t.Errorf("path %q served with response path %q, escaping %q", tc.query, payload.Path, adminStorageBrowseRoot)
			}
		})
	}
}
