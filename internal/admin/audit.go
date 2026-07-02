package admin

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// AuditEntry represents a single audit log record.
type AuditEntry struct {
	ID        uint           `json:"id"`
	UserID    string         `json:"user_id"`
	Username  string         `json:"username"`
	Action    string         `json:"action"`     // create, update, delete, login, logout
	ModelName string         `json:"model_name"` // Model affected (e.g. "User")
	RecordID  string         `json:"record_id"`  // ID of the affected record
	OldValue  map[string]any `json:"old_value"`  // Previous state (for updates)
	NewValue  map[string]any `json:"new_value"`  // New state (for creates/updates)
	IP        string         `json:"ip"`
	UserAgent string         `json:"user_agent"`
	CreatedAt time.Time      `json:"created_at"`
}

// auditStore is an in-memory bounded store for audit entries.
type auditStore struct {
	entries []AuditEntry
	maxSize int
	mu      sync.RWMutex
}

func newAuditStore(maxSize int) *auditStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &auditStore{
		entries: make([]AuditEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

func (s *auditStore) add(entry AuditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry.CreatedAt = time.Now().UTC()
	s.entries = append(s.entries, entry)

	// Trim if over max size
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}
}

func (s *auditStore) list(opts auditQueryOpts) []AuditEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Copy entries
	result := make([]AuditEntry, len(s.entries))
	copy(result, s.entries)

	// Apply filters
	if opts.UserID != "" {
		filtered := make([]AuditEntry, 0, len(result))
		for _, e := range result {
			if e.UserID == opts.UserID {
				filtered = append(filtered, e)
			}
		}
		result = filtered
	}
	if opts.ModelName != "" {
		filtered := make([]AuditEntry, 0, len(result))
		for _, e := range result {
			if e.ModelName == opts.ModelName {
				filtered = append(filtered, e)
			}
		}
		result = filtered
	}
	if opts.Action != "" {
		filtered := make([]AuditEntry, 0, len(result))
		for _, e := range result {
			if e.Action == opts.Action {
				filtered = append(filtered, e)
			}
		}
		result = filtered
	}

	// Sort by created_at desc
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Pagination
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 50
	}
	if opts.PageSize > 200 {
		opts.PageSize = 200
	}

	start := (opts.Page - 1) * opts.PageSize
	end := start + opts.PageSize
	if start >= len(result) {
		return []AuditEntry{}
	}
	if end > len(result) {
		end = len(result)
	}
	return result[start:end]
}

func (s *auditStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

type auditQueryOpts struct {
	UserID    string
	ModelName string
	Action    string
	Page      int
	PageSize  int
}

// recordAuditEntry records an audit log entry.
func (p *Panel) recordAuditEntry(r *http.Request, entry AuditEntry) {
	if p == nil || p.audit == nil {
		return
	}

	// Extract user info from context
	if user, _ := p.authenticatedUser(r); user != nil {
		entry.UserID = user.ID
		entry.Username = user.Username
	}

	// Extract request metadata
	entry.IP = auth.ClientIPFromRequest(r)
	entry.UserAgent = r.UserAgent()

	p.audit.add(entry)
}

// auditMiddleware returns a middleware that records audit entries for write operations.
func (p *Panel) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p == nil || p.audit == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Only record write operations (POST, PUT, DELETE)
		method := r.Method
		if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		// Extract model name from path
		modelName := r.PathValue("name")
		if modelName == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Determine action from method
		var action string
		switch method {
		case http.MethodPost:
			action = "create"
		case http.MethodPut, http.MethodPatch:
			action = "update"
		case http.MethodDelete:
			action = "delete"
		default:
			next.ServeHTTP(w, r)
			return
		}

		recordID := r.PathValue("id")

		// For updates, capture old value
		var oldValue map[string]any
		if action == "update" && recordID != "" {
			mi, ok := p.src.Get(modelName)
			if ok {
				databaseAlias, _ := p.requestDatabaseAlias(r)
				st, err := p.src.Store(mi.Name, databaseAlias)
				if err == nil {
					old, _ := st.Get(r.Context(), recordID)
					if old != nil {
						oldValue = old
					}
				}
			}
		}

		// Wrap response writer to capture status
		ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		// Only record successful operations
		if ww.Status() >= 200 && ww.Status() < 300 {
			entry := AuditEntry{
				Action:    action,
				ModelName: modelName,
				RecordID:  recordID,
				OldValue:  oldValue,
			}
			p.recordAuditEntry(r, entry)
		}
	})
}

// Admin audit log API handlers

func (p *Panel) handleListAuditLog(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "audit_view"); err != nil {
		return err
	}

	if p.audit == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled": false,
			"reason":  "Audit logging not enabled",
			"entries": []interface{}{},
			"total":   0,
		})
	}

	page, _, _ := parsePositiveQueryInt(c.Request.URL.Query(), "page")
	pageSize, _, _ := parsePositiveQueryInt(c.Request.URL.Query(), "page_size")
	userID := c.Query("user_id")
	modelName := c.Query("model")
	action := c.Query("action")

	entries := p.audit.list(auditQueryOpts{
		UserID:    userID,
		ModelName: modelName,
		Action:    action,
		Page:      page,
		PageSize:  pageSize,
	})

	total := p.audit.count()
	totalPages := int(float64(total)/float64(pageSize) + 0.99)
	if totalPages == 0 {
		totalPages = 1
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":     true,
		"entries":     entries,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

func (p *Panel) handleClearAuditLog(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "audit_manage"); err != nil {
		return err
	}

	if p.audit == nil {
		return gferrors.BadRequest("Audit logging not enabled")
	}

	p.audit.mu.Lock()
	p.audit.entries = p.audit.entries[:0]
	p.audit.mu.Unlock()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"cleared": true,
	})
}
