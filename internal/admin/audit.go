package admin

import (
	"encoding/csv"
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
	if opts.RecordID != "" && e.RecordID != opts.RecordID {
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

// auditDefaultPageSize and auditMaxPageSize are the page a caller gets when
// it asks for none and the largest one it can ask for. The cap is named
// rather than written at each allocation so that every place that sizes a
// buffer from a client-supplied page size can be seen to honour the same
// bound — including the export, which reads page after page.
const (
	auditDefaultPageSize = 50
	auditMaxPageSize     = 200
)

// normalizeAuditPage applies the defaults and the cap that both list and
// the HTTP handler use, so the page the response echoes is the page served.
func normalizeAuditPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = auditDefaultPageSize
	}
	if pageSize > auditMaxPageSize {
		pageSize = auditMaxPageSize
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

	// The capacity is a hint, so it is taken from what this store HOLDS and
	// capped by the page cap — never from the page size the request asked
	// for. Sizing a buffer from client input is worth avoiding even when the
	// value is bounded three functions earlier: the bound is easy to move,
	// the allocation is not easy to notice, and append grows the slice for
	// free when the hint is short.
	capacity := len(s.entries)
	if capacity > auditMaxPageSize {
		capacity = auditMaxPageSize
	}
	out := make([]AuditEntry, 0, capacity)
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
	// RecordID narrows the trail to one row, which is what the record
	// history of a model asks for: the same store, read by record.
	RecordID string
	Action   string
	Page     int
	PageSize int
}

// auditSink is what the panel writes its trail to. Two implementations: the
// in-memory ring (auditStore), which is what the panel always had, and the
// SQL one (sqlAuditStore), which survives the process — the difference an
// incident asks about and the reason the trail is worth keeping at all.
//
// Everything the panel does with the trail goes through this interface, so a
// handler never knows which one it is writing to; `persistent` exists only so
// the API can TELL a client which one it is reading, rather than leaving it
// to be discovered by a restart.
type auditSink interface {
	add(entry AuditEntry)
	list(opts auditQueryOpts) []AuditEntry
	count(opts auditQueryOpts) int
	clear() int
	// purge drops every entry older than before and returns how many went.
	purge(before time.Time) (int, error)
	persistent() bool
}

// purge drops the entries of the ring older than before. It is the same
// contract the SQL store honours; on the ring it is bounded work over a
// bounded slice.
func (s *auditStore) purge(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.entries[:0]
	dropped := 0
	for _, e := range s.entries {
		if e.CreatedAt.Before(before) {
			dropped++
			continue
		}
		kept = append(kept, e)
	}
	for i := len(kept); i < len(s.entries); i++ {
		s.entries[i] = AuditEntry{} // drop the references of the trimmed tail
	}
	s.entries = kept
	return dropped, nil
}

// persistent reports what this store is: a ring that lives and dies with the
// process.
func (s *auditStore) persistent() bool { return false }

// Retention: how long the trail is kept, as a PERIOD. The ring's own bound
// (AuditMaxSize) is a count of entries, which answers a different question —
// "how much do we hold" instead of "how far back do we go" — and a
// compliance window is always the second one.
const (
	auditStoreMemory   = "memory"
	auditStoreDatabase = "database"
	// auditPurgeInterval bounds how often a write triggers the retention
	// sweep. Retention is a window measured in days; sweeping on every entry
	// would spend a DELETE per write to drop nothing.
	auditPurgeInterval = time.Hour
)

// retentionDays is the window in effect, which an operator can change at
// runtime through the retention endpoint. Zero means "keep until cleared".
func (p *Panel) retentionDays() int {
	p.auditRetentionMu.RLock()
	defer p.auditRetentionMu.RUnlock()
	return p.auditRetention
}

// setRetentionDays changes the window in effect and applies it immediately.
// It does not write the application's configuration: the configured value is
// what a restart comes back to, and the endpoint says so.
func (p *Panel) setRetentionDays(days int) (dropped int, err error) {
	p.auditRetentionMu.Lock()
	p.auditRetention = days
	p.auditRetentionMu.Unlock()
	return p.purgeAuditNow()
}

// purgeAuditNow applies the retention window right away and reports how many
// entries went.
func (p *Panel) purgeAuditNow() (int, error) {
	days := p.retentionDays()
	if days <= 0 || p.audit == nil {
		return 0, nil
	}
	p.auditPurgeMu.Lock()
	p.lastAuditPurge = time.Now()
	p.auditPurgeMu.Unlock()
	return p.audit.purge(time.Now().UTC().AddDate(0, 0, -days))
}

// applyAuditRetention runs the window against a store the panel has just
// built (before it is installed), so a deploy that lowered the window does
// not wait for the next entry.
func (p *Panel) applyAuditRetention(sink auditSink) {
	days := p.config.AuditRetentionDays
	if days <= 0 || sink == nil {
		return
	}
	if _, err := sink.purge(time.Now().UTC().AddDate(0, 0, -days)); err != nil && p.logger != nil {
		p.logger.Warn("orbit: audit retention sweep failed", "error", err)
	}
}

// maybePurgeAudit applies the retention window at most once per
// auditPurgeInterval. It runs on the writing goroutine on purpose: a
// background sweeper would be one more thing to start, stop and leak, and a
// panel that records nothing needs no sweep at all.
func (p *Panel) maybePurgeAudit() {
	if p.retentionDays() <= 0 {
		return
	}
	p.auditPurgeMu.Lock()
	if time.Since(p.lastAuditPurge) < auditPurgeInterval {
		p.auditPurgeMu.Unlock()
		return
	}
	p.lastAuditPurge = time.Now()
	p.auditPurgeMu.Unlock()

	days := p.retentionDays()
	if dropped, err := p.audit.purge(time.Now().UTC().AddDate(0, 0, -days)); err != nil {
		if p.logger != nil {
			p.logger.Warn("orbit: audit retention sweep failed", "error", err)
		}
	} else if dropped > 0 && p.logger != nil {
		p.logger.Info("orbit: audit retention applied", "dropped", dropped, "days", days)
	}
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
	p.maybePurgeAudit()
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
		RecordID:  c.Query("record_id"),
		Action:    c.Query("action"),
		Page:      page,
		PageSize:  pageSize,
	}

	// A compliance request asks for a FILE, and answering it by scrolling a
	// screen is how a trail nobody can hand over stays a trail nobody can
	// hand over. The export carries the same filters as the listing, so what
	// is exported is what the operator was looking at.
	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "csv") {
		return p.writeAuditCSV(c, opts)
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
		// Whether this trail survives the process, said rather than left to
		// be discovered by a restart, and the window it is kept for.
		"persistent":     p.audit.persistent(),
		"retention_days": p.retentionDays(),
	})
}

// auditExportPageSize is how many entries one export page reads at a time.
// The export streams: a compliance window can be larger than memory.
//
// It is the LIST cap and not a number of its own: list() normalizes the page
// size it is given, so an export that asked for more got a short page back
// and read it as "that was the last one" — the copy stopped at the cap and
// said nothing. TestAuditSQL_ExportStreamsPastOnePage holds it down.
const auditExportPageSize = auditMaxPageSize

// writeAuditCSV streams the filtered trail as a CSV file. It is recorded in
// the trail itself — who took a copy of the log is exactly the kind of thing
// the log is for.
func (p *Panel) writeAuditCSV(c *router.Context, opts auditQueryOpts) error {
	w := c.Writer
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)

	writer := csv.NewWriter(w)
	if err := writer.Write([]string{
		"id", "created_at", "user_id", "username", "action",
		"model_name", "record_id", "old_value", "new_value", "ip", "user_agent",
	}); err != nil {
		return fmt.Errorf("audit export: write header: %w", err)
	}

	exported := 0
	page := opts
	page.PageSize = auditExportPageSize
	for page.Page = 1; ; page.Page++ {
		entries := p.audit.list(page)
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			if err := writer.Write([]string{
				strconv.FormatUint(uint64(e.ID), 10),
				e.CreatedAt.UTC().Format(time.RFC3339),
				e.UserID, e.Username, e.Action, e.ModelName, e.RecordID,
				auditValueText(e.OldValue), auditValueText(e.NewValue),
				e.IP, e.UserAgent,
			}); err != nil {
				return fmt.Errorf("audit export: write row: %w", err)
			}
			exported++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return fmt.Errorf("audit export: flush: %w", err)
		}
		if len(entries) < page.PageSize {
			break
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("audit export: flush: %w", err)
	}

	p.recordAuditEntry(c.Request, AuditEntry{
		Action:    "audit.export",
		ModelName: "audit",
		NewValue: map[string]any{
			"entries": exported,
			"filters": map[string]any{
				"user_id": opts.UserID, "model": opts.ModelName,
				"record_id": opts.RecordID, "action": opts.Action,
			},
		},
	})
	return nil
}

// auditValueText renders a value map for a CSV cell. The values are already
// redacted (redactAuditValues); this only has to make them one field.
func auditValueText(v map[string]any) string {
	if len(v) == 0 {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// handleAuditRetention reads the retention policy: how long the trail is
// kept, where it is kept, and what a restart comes back to.
func (p *Panel) handleAuditRetention(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "audit_view"); err != nil {
		return err
	}
	if p.audit == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled": false,
			"reason":  "Audit logging not enabled",
		})
	}
	return c.JSON(http.StatusOK, p.auditRetentionPayload())
}

func (p *Panel) auditRetentionPayload() map[string]interface{} {
	store := auditStoreMemory
	if p.audit != nil && p.audit.persistent() {
		store = auditStoreDatabase
	}
	return map[string]interface{}{
		"enabled":        true,
		"retention_days": p.retentionDays(),
		// What the application configured, so an operator who changed the
		// window at runtime can see what a restart comes back to.
		"configured_retention_days": p.config.AuditRetentionDays,
		"store":                     store,
		"persistent":                p.audit != nil && p.audit.persistent(),
		"max_entries":               p.config.AuditMaxSize,
	}
}

// handleSetAuditRetention declares how long the trail is kept, and applies it
// at once. It changes the window IN EFFECT, not the application's
// configuration file: a restart comes back to the configured value, and the
// payload says so rather than letting an operator believe otherwise.
func (p *Panel) handleSetAuditRetention(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "audit_manage"); err != nil {
		return err
	}
	if p.audit == nil {
		return gferrors.BadRequest("Audit logging not enabled")
	}

	var req struct {
		RetentionDays *int `json:"retention_days"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	if req.RetentionDays == nil {
		return gferrors.BadRequest("retention_days is required (0 keeps entries until the log is cleared)")
	}
	days := *req.RetentionDays
	if days < 0 {
		return gferrors.BadRequest("retention_days cannot be negative")
	}
	if days > maxAuditRetentionDays {
		return gferrors.BadRequest(fmt.Sprintf("retention_days cannot exceed %d", maxAuditRetentionDays))
	}

	previous := p.retentionDays()
	dropped, err := p.setRetentionDays(days)
	if err != nil {
		return err
	}

	p.recordAuditEntry(c.Request, AuditEntry{
		Action:    "audit.retention.set",
		ModelName: "audit",
		OldValue:  map[string]any{"retention_days": previous},
		NewValue:  map[string]any{"retention_days": days, "dropped": dropped},
	})

	payload := p.auditRetentionPayload()
	payload["dropped"] = dropped
	return c.JSON(http.StatusOK, payload)
}

// maxAuditRetentionDays is a century: long enough for any real policy, short
// enough that a typo does not overflow the date arithmetic.
const maxAuditRetentionDays = 36500

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

// handleRecordHistory answers what an operator asks after a bad edit: what
// did this row look like before, and who changed it.
//
// It is the audit trail read by record, not a second store: the entries are
// already there, with both sides of every edit. That is also its limit, and
// the payload says so — the history goes back as far as the trail does, so a
// panel on the in-memory ring answers "since this process started" and one on
// the database answers "since the retention window".
//
// Who may read it is the record's own permission, not the log's: an operator
// who may retrieve the row may see what it said before. The row scope and the
// field permissions of that grant apply here too — a history that showed the
// values of a row the operator cannot open, or a field they may not read,
// would be a way around both.
func (p *Panel) handleRecordHistory(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	rowScope, err := p.authorizeRecordAction(c, mi, "retrieve")
	if err != nil {
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

	// The row has to be one this operator can reach: a row-scoped operator
	// asking for somebody else's history gets the same answer as for the row
	// itself.
	if rowScope.Enforced() {
		databaseAlias, err := p.requestDatabaseAlias(r)
		if err != nil {
			return gferrors.BadRequest(err.Error())
		}
		if mi.DatabaseAlias != "" && r.URL.Query().Get("db") == "" &&
			r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
			databaseAlias = mi.DatabaseAlias
		}
		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return err
		}
		if err := scopedOwnedRecord(r.Context(), st, mi, idStr, rowScope); err != nil {
			return err
		}
	}

	page, _, _ := parsePositiveQueryInt(r.URL.Query(), "page")
	pageSize, _, _ := parsePositiveQueryInt(r.URL.Query(), "page_size")
	page, pageSize = normalizeAuditPage(page, pageSize)

	opts := auditQueryOpts{
		ModelName: mi.Name,
		RecordID:  idStr,
		Page:      page,
		PageSize:  pageSize,
	}
	entries := p.audit.list(opts)

	// A field this operator may not read is not readable through its own
	// history either.
	if rules := p.requestFieldRules(r, mi); rules.enforced() {
		for i := range entries {
			entries[i].OldValue = rules.maskValues(mi, entries[i].OldValue)
			entries[i].NewValue = rules.maskValues(mi, entries[i].NewValue)
		}
	}

	total := p.audit.count(opts)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":     true,
		"model":       mi.Name,
		"record_id":   idStr,
		"entries":     entries,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
		// How far back this history can possibly go, so a gap is read as
		// what it is instead of as "nothing happened".
		"persistent":     p.audit.persistent(),
		"retention_days": p.retentionDays(),
	})
}
