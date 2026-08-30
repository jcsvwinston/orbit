package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
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
	// nextID is a monotonically increasing entry id, assigned in add(). It
	// keeps ids unique for the life of the process even after the ring trims
	// old entries or the log is cleared — a consumer can address an entry by
	// id instead of every entry reporting id 0.
	nextID uint
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

	s.nextID++
	entry.ID = s.nextID
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
		mi, hasModel := p.src.Get(modelName)

		// For updates, capture old value — redacted: excluded fields and
		// credential-shaped names never reach the audit store, which is
		// readable via /api/audit (see redactAuditValues).
		var oldValue map[string]any
		if action == "update" && recordID != "" && hasModel {
			databaseAlias, _ := p.requestDatabaseAlias(r)
			st, err := p.src.Store(mi.Name, databaseAlias)
			if err == nil {
				old, _ := st.Get(r.Context(), recordID)
				if old != nil {
					oldValue = redactAuditValues(mi, old)
				}
			}
		}

		// Wrap response writer to capture status. A create has no id in the
		// path (the database assigns it), so its response body — the created
		// record — is additionally captured (bounded) to fill record_id.
		ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
		var out http.ResponseWriter = ww
		var capture *auditBodyCapture
		if action == "create" && recordID == "" {
			capture = &auditBodyCapture{WrapResponseWriter: ww}
			out = capture
		}
		next.ServeHTTP(out, r)

		// Only record successful operations
		if ww.Status() >= 200 && ww.Status() < 300 {
			if capture != nil && hasModel {
				recordID = extractCreatedRecordID(mi, capture.buf.Bytes())
			}
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

// auditCreateBodyCaptureLimit bounds how much of a create response the audit
// middleware buffers to read the assigned record id out of it.
const auditCreateBodyCaptureLimit = 64 << 10

// auditBodyCapture tees the response body (bounded) while delegating the
// actual write — status tracking included — to the wrapped writer.
type auditBodyCapture struct {
	*router.WrapResponseWriter
	buf bytes.Buffer
}

func (w *auditBodyCapture) Write(b []byte) (int, error) {
	if w.buf.Len() < auditCreateBodyCaptureLimit {
		room := auditCreateBodyCaptureLimit - w.buf.Len()
		if room > len(b) {
			room = len(b)
		}
		w.buf.Write(b[:room])
	}
	return w.WrapResponseWriter.Write(b)
}

// extractCreatedRecordID reads the primary-key value out of a create
// response's JSON body (the created record). It tries the model's declared
// primary key (Go name and column) and falls back to the conventional "id"
// key; a body that is not a JSON object yields "".
func extractCreatedRecordID(mi datasource.ModelInfo, body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var rec map[string]any
	if err := dec.Decode(&rec); err != nil {
		return ""
	}

	candidates := make([]string, 0, 4)
	if pk := strings.TrimSpace(mi.PrimaryKey); pk != "" {
		candidates = append(candidates, pk)
	}
	for _, f := range mi.Fields {
		if f.IsPK {
			candidates = append(candidates, f.Name, f.Column)
		}
	}
	candidates = append(candidates, "id")

	for _, want := range candidates {
		for k, v := range rec {
			if !strings.EqualFold(k, want) {
				continue
			}
			if s := auditScalarString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// auditScalarString renders a JSON scalar as a record id string; composite
// values (objects, arrays, booleans, null) yield "".
func auditScalarString(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return strings.TrimSpace(n)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	default:
		return ""
	}
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

	// Normalize BEFORE computing pagination, mirroring what list() applies —
	// dividing by an absent page_size (0) used to overflow total_pages to
	// MaxInt64. The normalized values are also what the response echoes, so
	// page/page_size/total_pages are consistent with the entries returned.
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	entries := p.audit.list(auditQueryOpts{
		UserID:    userID,
		ModelName: modelName,
		Action:    action,
		Page:      page,
		PageSize:  pageSize,
	})

	total := p.audit.count()
	totalPages := (total + pageSize - 1) / pageSize
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
