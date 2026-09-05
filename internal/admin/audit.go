package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// AuditEntry represents a single audit log record.
//
// Every mutating handler of the panel records its own entry (see
// audit_coverage_test.go for the route-by-route contract). Action is the
// verb: Data Studio uses create/update/delete/bulk_delete/bulk_export and
// schema.update; the management surfaces use dotted names (rbac.policy.add,
// flag.set, migration.apply, cache.flush, export.create, import.execute,
// live.exclude.add, audit.clear, ...); the session surfaces use login,
// login.failed, login.locked, logout and session.terminate.
type AuditEntry struct {
	ID        uint           `json:"id"`
	UserID    string         `json:"user_id"`
	Username  string         `json:"username"`
	Action    string         `json:"action"`
	ModelName string         `json:"model_name"` // Model or surface affected (e.g. "User", "rbac", "feature_flag")
	RecordID  string         `json:"record_id"`  // ID of the affected record (or key / name of the affected object)
	OldValue  map[string]any `json:"old_value"`  // Previous state (updates, deletes, removals) — redacted
	NewValue  map[string]any `json:"new_value"`  // New state or outcome (creates, updates, management actions) — redacted
	IP        string         `json:"ip"`
	UserAgent string         `json:"user_agent"`
	CreatedAt time.Time      `json:"created_at"`
}

// matches reports whether the entry satisfies every set filter of opts; an
// empty filter is a wildcard.
func (e *AuditEntry) matches(opts auditQueryOpts) bool {
	if opts.UserID != "" && e.UserID != opts.UserID {
		return false
	}
	if opts.ModelName != "" && e.ModelName != opts.ModelName {
		return false
	}
	if opts.Action != "" && e.Action != opts.Action {
		return false
	}
	return true
}

// auditStore is an in-memory bounded store for audit entries.
//
// Entries are kept in insertion order, which is chronological: add assigns
// CreatedAt and the id under the same lock, so the newest entry is always
// the last one. list walks the slice backwards and stops once it has a
// page, instead of copying and sorting the whole ring per request.
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

// add stores entry with the next id and the current time. The string
// fields are cut to their bounds here, at the one point every writer goes
// through, so the ring's footprint is bounded by its size and not by what a
// request carried (the login route writes here unauthenticated).
func (s *auditStore) add(entry AuditEntry) {
	entry = boundAuditEntry(entry)

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

// normalizeAuditPage applies the defaults and the cap that both list and
// the HTTP handler use, so the page the response echoes is the page served.
func normalizeAuditPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

// list returns one page of entries, newest first, honouring the filters of
// opts. It never copies the ring: it walks from the newest entry backwards,
// skips the entries before the requested page and stops as soon as the page
// is full. Newest-first is deterministic because ids are assigned in
// insertion order (two entries never tie).
func (s *auditStore) list(opts auditQueryOpts) []AuditEntry {
	page, pageSize := normalizeAuditPage(opts.Page, opts.PageSize)
	skip := (page - 1) * pageSize

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]AuditEntry, 0, min(pageSize, len(s.entries)))
	for i := len(s.entries) - 1; i >= 0 && len(out) < pageSize; i-- {
		e := &s.entries[i]
		if !e.matches(opts) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		out = append(out, *e)
	}
	return out
}

// count returns how many entries satisfy the filters of opts (all of them
// when no filter is set), so total/total_pages agree with the entries a
// filtered listing serves.
func (s *auditStore) count(opts auditQueryOpts) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if opts.UserID == "" && opts.ModelName == "" && opts.Action == "" {
		return len(s.entries)
	}
	n := 0
	for i := range s.entries {
		if s.entries[i].matches(opts) {
			n++
		}
	}
	return n
}

// clear empties the ring and returns how many entries it dropped. Ids keep
// growing across a clear (nextID is untouched).
func (s *auditStore) clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.entries)
	clear(s.entries) // drop the references so the values can be collected
	s.entries = s.entries[:0]
	return n
}

type auditQueryOpts struct {
	UserID    string
	ModelName string
	Action    string
	Page      int
	PageSize  int
}

// recordAuditEntry records an audit log entry attributed to the operator
// of the request. A caller that already names the actor (login records the
// attempted username before any session exists) is taken as-is, so a failed
// login does not cost an Auth.Authenticate round-trip per attempt.
func (p *Panel) recordAuditEntry(r *http.Request, entry AuditEntry) {
	if p == nil || p.audit == nil {
		return
	}

	if entry.UserID == "" && entry.Username == "" && r != nil {
		if user, _ := p.authenticatedUser(r); user != nil {
			entry.UserID = user.ID
			entry.Username = user.Username
		}
	}

	p.addAuditEntry(r, entry)
}

// addAuditEntry stamps the request metadata on entry and stores it without
// resolving the operator.
func (p *Panel) addAuditEntry(r *http.Request, entry AuditEntry) {
	if p == nil || p.audit == nil {
		return
	}
	if r != nil {
		entry.IP = auth.ClientIPFromRequest(r)
		entry.UserAgent = r.UserAgent()
	}
	p.audit.add(entry)
}

// auditLogin wraps the auth provider's login handler so that every POST
// attempt leaves an audit entry — "login" when the provider redirected
// (success), "login.failed" when it answered 401/403 and "login.locked" when
// the lockout answered 429 — carrying the attempted username and never the
// password. Other statuses (a malformed form, a provider failure) record
// nothing: no credential was checked. The provider itself stays unaware of
// the audit store.
//
// The route is unauthenticated, so what one client can write to the ring
// is bounded: the entry's strings are cut by the store, the body by
// limitLoginBody, and per client IP and loginFailureWindow the log keeps at
// most loginFailureLimit login.failed entries and one login.locked (see
// loginAuditAllowed). Without that budget the lockout's 429, recorded on
// every request, let one client evict the whole ring at the request rate.
func (p *Panel) auditLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p == nil || p.audit == nil || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		var action string
		switch status := ww.Status(); {
		case status >= 300 && status < 400:
			action = "login"
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			action = "login.failed"
		case status == http.StatusTooManyRequests:
			action = "login.locked"
		default:
			return
		}
		if !p.loginAuditAllowed(action, auth.ClientIPFromRequest(r)) {
			return
		}

		// The handler already parsed the form; FormValue is idempotent and
		// the password is never read here.
		entry := AuditEntry{
			Action:   action,
			Username: strings.TrimSpace(r.FormValue("username")),
		}
		if action == "login" && p.config.Session != nil && sessionContextReady(p.config.Session, r.Context()) {
			entry.UserID = strings.TrimSpace(p.config.Session.GetString(r.Context(), adminSessionUserIDKey))
		}
		if action == "login" && entry.Username == "" && entry.UserID == "" {
			// A provider that does not post a username form field: fall
			// back to the session the login just established.
			p.recordAuditEntry(r, entry)
			return
		}
		p.addAuditEntry(r, entry)
	})
}

// loginAuditAllowed applies the per-client budget of login entries and
// reports whether this attempt's entry is recorded. Per client IP and
// loginFailureWindow (a fixed window, like the lockout's) it admits
// loginFailureLimit login.failed entries and one login.locked: the entries
// already in the ring document the attack, and the 429 the lockout keeps
// answering adds nothing. A successful login from the IP resets its budget,
// as it resets the lockout. When the budget cannot track a client (at
// loginLimiterCap keys) the entry is recorded — fail-open, like the lockout.
func (p *Panel) loginAuditAllowed(action, ip string) bool {
	budget := p.loginAuditBudget // nil-safe: a nil limiter tracks nothing
	failedKey, lockedKey := "failed:"+ip, "locked:"+ip
	switch action {
	case "login":
		budget.reset(failedKey)
		budget.reset(lockedKey)
		return true
	case "login.failed":
		return budget.fail(failedKey) <= loginFailureLimit
	case "login.locked":
		return budget.fail(lockedKey) <= 1
	}
	return true
}

// Bounds on the strings an audit entry stores, in bytes. A record's values
// are cut at auditValueMaxLen (a large text column must not multiply the
// ring's footprint); the identity and request fields at auditFieldMaxLen
// and the User-Agent at auditUserAgentMaxLen. The login route reaches the
// store unauthenticated, so the attempted username and the request headers
// are the one place an anonymous client chooses what an entry holds.
const (
	auditValueMaxLen     = 4096
	auditFieldMaxLen     = 256
	auditUserAgentMaxLen = 512
)

// auditTruncatedMarker ends every string the store cut, so a reader can
// tell a bounded value from a short one.
const auditTruncatedMarker = "…[truncated]"

// truncateAuditString cuts s to at most maxLen bytes on a rune boundary and
// appends auditTruncatedMarker; a string within the bound is returned as is.
func truncateAuditString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + auditTruncatedMarker
}

// boundAuditEntry cuts the string fields of entry to their bounds. Action
// is not cut: every writer sets it to a literal or a validated name.
func boundAuditEntry(entry AuditEntry) AuditEntry {
	entry.UserID = truncateAuditString(entry.UserID, auditFieldMaxLen)
	entry.Username = truncateAuditString(entry.Username, auditFieldMaxLen)
	entry.ModelName = truncateAuditString(entry.ModelName, auditFieldMaxLen)
	entry.RecordID = truncateAuditString(entry.RecordID, auditFieldMaxLen)
	entry.IP = truncateAuditString(entry.IP, auditFieldMaxLen)
	entry.UserAgent = truncateAuditString(entry.UserAgent, auditUserAgentMaxLen)
	return entry
}

// auditValues prepares a record's values for the audit store: excluded and
// credential-shaped fields are redacted (redactAuditValues) and long strings
// are truncated. A nil record stays nil.
func auditValues(mi datasource.ModelInfo, rec datasource.Record) map[string]any {
	if rec == nil {
		return nil
	}
	return boundAuditValues(redactAuditValues(mi, rec))
}

// boundAuditValues truncates string values longer than auditValueMaxLen in
// place (on the rune boundary) and returns the map.
func boundAuditValues(values map[string]any) map[string]any {
	for k, v := range values {
		if s, ok := v.(string); ok && len(s) > auditValueMaxLen {
			values[k] = truncateAuditString(s, auditValueMaxLen)
		}
	}
	return values
}

// auditJSONValues renders a struct (a feature flag row, a report) as the
// generic map an audit entry stores, through its JSON encoding so the keys
// match what the API serves. A value that does not encode to an object
// yields nil.
func auditJSONValues(v any) map[string]any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// auditRecordID renders a record's primary-key value as the audit record_id.
// It tries the model's declared primary key (Go name and column) and falls
// back to the conventional "id" key; a record without a scalar key yields "".
func auditRecordID(mi datasource.ModelInfo, rec datasource.Record) string {
	if rec == nil {
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

// auditScalarString renders a scalar as a record id string; composite
// values (objects, arrays, booleans, null) yield "".
func auditScalarString(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return strings.TrimSpace(n)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(n)
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

	// Normalize BEFORE computing pagination, mirroring what list() applies —
	// dividing by an absent page_size (0) used to overflow total_pages to
	// MaxInt64. The normalized values are also what the response echoes, so
	// page/page_size/total_pages are consistent with the entries returned.
	page, pageSize = normalizeAuditPage(page, pageSize)

	opts := auditQueryOpts{
		UserID:    c.Query("user_id"),
		ModelName: c.Query("model"),
		Action:    c.Query("action"),
		Page:      page,
		PageSize:  pageSize,
	}
	entries := p.audit.list(opts)

	// total counts the entries that match the filters, so a filtered
	// listing does not page past its last entry.
	total := p.audit.count(opts)
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

	cleared := p.audit.clear()

	// Recorded AFTER the clear so the wipe itself survives it: the ring
	// then holds exactly one entry saying who emptied it and how much.
	p.recordAuditEntry(c.Request, AuditEntry{
		Action:    "audit.clear",
		ModelName: "audit",
		NewValue:  map[string]any{"cleared": cleared},
	})

	return c.JSON(http.StatusOK, map[string]interface{}{
		"cleared": true,
		"dropped": cleared,
	})
}
