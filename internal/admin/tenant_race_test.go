package admin

import (
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// TestResolveTenantField_ConcurrentAccess exercises the tenant-field cache from
// several goroutines at once, the way concurrent operators (or one operator
// with several tabs) hit handleGetSchema/handleListRecords in production. The
// cache is a plain map; without synchronization this is a fatal
// "concurrent map read and map write" that crashes the HOST process — run
// with -race to catch it deterministically (AO-1).
func TestResolveTenantField_ConcurrentAccess(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	const (
		goroutines = 8
		iterations = 200
	)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Alternate a registered model with an unknown one so both the
				// hit path (read) and the miss path (write) run concurrently.
				_ = panel.resolveTenantField("AdminUser")
				_ = panel.resolveTenantField("NoSuchModel")
			}
		}()
	}
	wg.Wait()
}
