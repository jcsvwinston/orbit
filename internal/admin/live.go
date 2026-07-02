package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"golang.org/x/net/websocket"
)

const (
	defaultLiveRequestBufferSize = 256
	defaultLiveSQLBufferSize     = 256
	defaultLiveSubscriberBuffer  = 128
	defaultLiveSessionTTL        = 30 * time.Minute
	defaultLiveListLimit         = 50
	maxLiveListLimit             = 1000
	maxLiveSQLArgs               = 16
	liveNodeOnlineWindow         = 45 * time.Second
	liveNodeDegradedWindow       = 3 * time.Minute
)

type liveTrafficObservedKey struct{}

type liveRuntime struct {
	requests *requestRingBuffer
	sql      *sqlQueryRingBuffer
	bus      *liveEventBus
	sessions *liveSessionStore
	nodes    *liveNodeStore
}

type liveSnapshotResponse struct {
	Enabled          bool                   `json:"enabled"`
	GeneratedAt      string                 `json:"generated_at"`
	Limit            int                    `json:"limit"`
	NodeFilter       string                 `json:"node_filter,omitempty"`
	TraceURLTemplate string                 `json:"trace_url_template,omitempty"`
	RequestLimit     int                    `json:"request_limit"`
	SQLLimit         int                    `json:"sql_limit"`
	SessionLimit     int                    `json:"session_limit"`
	ExcludePatterns  []string               `json:"exclude_patterns"`
	Nodes            []liveNodeSnapshot     `json:"nodes"`
	Requests         []liveRequestEvent     `json:"requests"`
	Queries          []liveSQLEvent         `json:"queries"`
	Sessions         []liveSessionActivity  `json:"sessions"`
	Stream           liveStreamStats        `json:"stream"`
	RequestBuffer    liveRequestBufferStats `json:"request_buffer"`
	SQLBuffer        liveRequestBufferStats `json:"sql_buffer"`
	Cluster          liveClusterSnapshot    `json:"cluster"`
}

type liveNodeSnapshot struct {
	NodeID        string `json:"node_id"`
	LastSeenAt    string `json:"last_seen_at,omitempty"`
	LastEventType string `json:"last_event_type,omitempty"`
	Requests      int    `json:"requests"`
	SQLQueries    int    `json:"sql_queries"`
	Sessions      int    `json:"sessions"`
	Status        string `json:"status"`
}

type liveRequestBufferStats struct {
	Capacity int `json:"capacity"`
	Stored   int `json:"stored"`
}

type liveStreamStats struct {
	Subscribers int    `json:"subscribers"`
	Published   uint64 `json:"published"`
	Dropped     uint64 `json:"dropped"`
}

type liveRequestEvent struct {
	NodeID         string `json:"node_id,omitempty"`
	Timestamp      string `json:"timestamp"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Status         int    `json:"status"`
	DurationMS     int64  `json:"duration_ms"`
	RequestID      string `json:"request_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	RemoteIP       string `json:"remote_ip,omitempty"`
	UserAgent      string `json:"user_agent,omitempty"`
	PayloadPreview string `json:"payload_preview,omitempty"`
}

type liveSessionActivity struct {
	NodeID       string `json:"node_id,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	TokenShort   string `json:"token_short"`
	UserID       string `json:"user_id,omitempty"`
	IP           string `json:"ip,omitempty"`
	UserAgent    string `json:"user_agent,omitempty"`
	LastRoute    string `json:"last_route"`
	LastSeenAt   string `json:"last_seen_at"`
	TraceID      string `json:"trace_id,omitempty"`
}

type liveSQLEvent struct {
	NodeID     string   `json:"node_id,omitempty"`
	Timestamp  string   `json:"timestamp"`
	ModelName  string   `json:"model_name,omitempty"`
	Operation  string   `json:"operation"`
	Query      string   `json:"query"`
	Args       []string `json:"args,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
	TraceID    string   `json:"trace_id,omitempty"`
	UserID     string   `json:"user_id,omitempty"`
}

type liveEventEnvelope struct {
	NodeID    string               `json:"node_id,omitempty"`
	Type      string               `json:"type"`
	Timestamp string               `json:"timestamp"`
	Request   *liveRequestEvent    `json:"request,omitempty"`
	Session   *liveSessionActivity `json:"session,omitempty"`
	SQL       *liveSQLEvent        `json:"sql,omitempty"`
}

type liveClusterSnapshot struct {
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	NodeID    string `json:"node_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Published uint64 `json:"published"`
	Dropped   uint64 `json:"dropped"`
	Received  uint64 `json:"received"`
	Ignored   uint64 `json:"ignored"`
}

type requestRingBuffer struct {
	mu       sync.RWMutex
	events   []liveRequestEvent
	head     int
	size     int
	capacity int
}

type sqlQueryRingBuffer struct {
	mu       sync.RWMutex
	events   []liveSQLEvent
	head     int
	size     int
	capacity int
}

type liveEventBus struct {
	mu             sync.RWMutex
	nextID         uint64
	subscriberSize int
	subscribers    map[uint64]chan liveEventEnvelope
	published      atomic.Uint64
	dropped        atomic.Uint64
}

type liveSessionStore struct {
	mu      sync.RWMutex
	entries map[string]liveSessionActivity
	ttl     time.Duration
}

func newLiveRuntime() *liveRuntime {
	return &liveRuntime{
		requests: newRequestRingBuffer(defaultLiveRequestBufferSize),
		sql:      newSQLQueryRingBuffer(defaultLiveSQLBufferSize),
		bus:      newLiveEventBus(defaultLiveSubscriberBuffer),
		sessions: newLiveSessionStore(defaultLiveSessionTTL),
		nodes:    newLiveNodeStore(),
	}
}

type liveNodeStore struct {
	mu    sync.RWMutex
	nodes map[string]time.Time
}

func newLiveNodeStore() *liveNodeStore {
	return &liveNodeStore{nodes: make(map[string]time.Time)}
}

func (s *liveNodeStore) touch(nodeID string, timestamp time.Time) {
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	s.nodes[nodeID] = timestamp
	s.mu.Unlock()
	slog.Info("node presence touched", "node", nodeID, "ts", timestamp.Format(time.RFC3339))
}

func (s *liveNodeStore) active(window time.Duration) map[string]time.Time {
	if s == nil {
		return nil
	}
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]time.Time)
	for id, last := range s.nodes {
		if now.Sub(last) <= window {
			out[id] = last
		}
	}
	return out
}

func newRequestRingBuffer(capacity int) *requestRingBuffer {
	if capacity <= 0 {
		capacity = defaultLiveRequestBufferSize
	}
	return &requestRingBuffer{
		events:   make([]liveRequestEvent, capacity),
		capacity: capacity,
	}
}

func newSQLQueryRingBuffer(capacity int) *sqlQueryRingBuffer {
	if capacity <= 0 {
		capacity = defaultLiveSQLBufferSize
	}
	return &sqlQueryRingBuffer{
		events:   make([]liveSQLEvent, capacity),
		capacity: capacity,
	}
}

func (rb *requestRingBuffer) push(event liveRequestEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.events[rb.head] = event
	rb.head = (rb.head + 1) % rb.capacity
	if rb.size < rb.capacity {
		rb.size++
	}
}

func (rb *requestRingBuffer) latest(limit int) []liveRequestEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 || limit <= 0 {
		return []liveRequestEvent{}
	}
	if limit > rb.size {
		limit = rb.size
	}

	out := make([]liveRequestEvent, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (rb.head - 1 - i + rb.capacity) % rb.capacity
		out = append(out, rb.events[idx])
	}
	return out
}

func (rb *requestRingBuffer) latestFiltered(limit int, patterns []string) []liveRequestEvent {
	return rb.latestFilteredByNode(limit, patterns, "")
}

func (rb *requestRingBuffer) latestFilteredByNode(limit int, patterns []string, nodeID string) []liveRequestEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 || limit <= 0 {
		return []liveRequestEvent{}
	}
	if limit > rb.size {
		limit = rb.size
	}

	targetNode := strings.TrimSpace(nodeID)
	out := make([]liveRequestEvent, 0, limit)
	for i := 0; i < rb.size && len(out) < limit; i++ {
		idx := (rb.head - 1 - i + rb.capacity) % rb.capacity
		row := rb.events[idx]
		if shouldExcludeLivePath(row.Path, patterns) {
			continue
		}
		if targetNode != "" && !strings.EqualFold(strings.TrimSpace(row.NodeID), targetNode) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (rb *requestRingBuffer) stats() liveRequestBufferStats {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return liveRequestBufferStats{
		Capacity: rb.capacity,
		Stored:   rb.size,
	}
}

func (rb *sqlQueryRingBuffer) push(event liveSQLEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.events[rb.head] = event
	rb.head = (rb.head + 1) % rb.capacity
	if rb.size < rb.capacity {
		rb.size++
	}
}

func (rb *sqlQueryRingBuffer) latest(limit int) []liveSQLEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 || limit <= 0 {
		return []liveSQLEvent{}
	}
	if limit > rb.size {
		limit = rb.size
	}

	out := make([]liveSQLEvent, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (rb.head - 1 - i + rb.capacity) % rb.capacity
		out = append(out, rb.events[idx])
	}
	return out
}

func (rb *sqlQueryRingBuffer) stats() liveRequestBufferStats {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return liveRequestBufferStats{
		Capacity: rb.capacity,
		Stored:   rb.size,
	}
}

func newLiveEventBus(subscriberSize int) *liveEventBus {
	if subscriberSize <= 0 {
		subscriberSize = defaultLiveSubscriberBuffer
	}
	return &liveEventBus{
		subscriberSize: subscriberSize,
		subscribers:    make(map[uint64]chan liveEventEnvelope),
	}
}

func (b *liveEventBus) subscribe() (<-chan liveEventEnvelope, func()) {
	id := atomic.AddUint64(&b.nextID, 1)
	ch := make(chan liveEventEnvelope, b.subscriberSize)

	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		existing, ok := b.subscribers[id]
		if ok {
			delete(b.subscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func (b *liveEventBus) publish(event liveEventEnvelope) {
	if b == nil {
		return
	}
	b.published.Add(1)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			b.dropped.Add(1)
		}
	}
}

func (b *liveEventBus) stats() liveStreamStats {
	if b == nil {
		return liveStreamStats{}
	}
	b.mu.RLock()
	subs := len(b.subscribers)
	b.mu.RUnlock()
	return liveStreamStats{
		Subscribers: subs,
		Published:   b.published.Load(),
		Dropped:     b.dropped.Load(),
	}
}

func newLiveSessionStore(ttl time.Duration) *liveSessionStore {
	if ttl <= 0 {
		ttl = defaultLiveSessionTTL
	}
	return &liveSessionStore{
		entries: make(map[string]liveSessionActivity),
		ttl:     ttl,
	}
}

func (s *liveSessionStore) upsert(key string, value liveSessionActivity) {
	if s == nil || strings.TrimSpace(key) == "" {
		return
	}
	s.mu.Lock()
	s.entries[key] = value
	s.gcLocked(time.Now().UTC())
	s.mu.Unlock()
}

func (s *liveSessionStore) snapshot(limit int) []liveSessionActivity {
	if s == nil || limit <= 0 {
		return []liveSessionActivity{}
	}

	now := time.Now().UTC()
	s.mu.Lock()
	s.gcLocked(now)

	rows := make([]liveSessionActivity, 0, len(s.entries))
	for _, row := range s.entries {
		rows = append(rows, row)
	}
	s.mu.Unlock()

	sort.SliceStable(rows, func(i, j int) bool {
		ti := parseRFC3339(rows[i].LastSeenAt)
		tj := parseRFC3339(rows[j].LastSeenAt)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return rows[i].TokenShort < rows[j].TokenShort
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func (s *liveSessionStore) gcLocked(now time.Time) {
	if s == nil {
		return
	}
	for key, row := range s.entries {
		seenAt := parseRFC3339(row.LastSeenAt)
		if seenAt.IsZero() || now.Sub(seenAt) <= s.ttl {
			continue
		}
		delete(s.entries, key)
	}
}

func parseRFC3339(raw string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func (p *Panel) liveTrafficMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p == nil || p.live == nil || r == nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.Context().Value(liveTrafficObservedKey{}) != nil {
			next.ServeHTTP(w, r)
			return
		}
		if shouldExcludeLivePath(r.URL.Path, p.liveExcludePatterns()) {
			next.ServeHTTP(w, r.WithContext(contextWithLiveObserved(r)))
			return
		}
		if router.IsWebSocketUpgrade(r) {
			next.ServeHTTP(w, r.WithContext(contextWithLiveObserved(r)))
			return
		}

		start := time.Now()
		ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(contextWithLiveObserved(r)))
		p.recordLiveRequest(r, ww.Status(), time.Since(start))
	})
}

func contextWithLiveObserved(r *http.Request) context.Context {
	if r == nil {
		return nil
	}
	return context.WithValue(r.Context(), liveTrafficObservedKey{}, true)
}

func (p *Panel) recordLiveRequest(r *http.Request, status int, duration time.Duration) {
	if p == nil || p.live == nil || r == nil {
		return
	}

	now := time.Now().UTC()
	ctx := r.Context()
	event := liveRequestEvent{
		NodeID:         p.liveNodeID(),
		Timestamp:      now.Format(time.RFC3339),
		Method:         r.Method,
		Path:           truncateText(r.URL.Path, 240),
		Status:         status,
		DurationMS:     duration.Milliseconds(),
		RequestID:      strings.TrimSpace(observe.RequestIDFromCtx(ctx)),
		TraceID:        strings.TrimSpace(observe.TraceIDFromCtx(ctx)),
		UserID:         strings.TrimSpace(observe.UserIDFromCtx(ctx)),
		RemoteIP:       auth.ClientIPFromRequest(r),
		UserAgent:      truncateText(strings.TrimSpace(r.UserAgent()), 320),
		PayloadPreview: livePayloadPreview(r),
	}
	p.live.requests.push(event)
	envelope := liveEventEnvelope{
		NodeID:    event.NodeID,
		Type:      "http.request",
		Timestamp: event.Timestamp,
		Request:   &event,
	}
	p.live.bus.publish(envelope)
	p.publishLiveClusterEvent(envelope)

	p.recordLiveSessionActivity(r, now, event.TraceID)
}

func (p *Panel) onModelSQLQuery(ctx context.Context, queryEvent model.SQLQueryEvent) {
	if p == nil || p.live == nil {
		return
	}
	now := time.Now().UTC()
	event := liveSQLEvent{
		NodeID:     p.liveNodeID(),
		Timestamp:  now.Format(time.RFC3339),
		ModelName:  strings.TrimSpace(queryEvent.ModelName),
		Operation:  truncateText(strings.TrimSpace(queryEvent.Operation), 64),
		Query:      truncateText(compactSQL(queryEvent.Query), 640),
		Args:       sanitizeLiveSQLArgs(queryEvent.Args),
		DurationMS: queryEvent.Duration.Milliseconds(),
		RequestID:  strings.TrimSpace(observe.RequestIDFromCtx(ctx)),
		TraceID:    strings.TrimSpace(observe.TraceIDFromCtx(ctx)),
		UserID:     strings.TrimSpace(observe.UserIDFromCtx(ctx)),
	}
	if queryEvent.Error != nil {
		event.Error = truncateText(queryEvent.Error.Error(), 220)
	}
	p.live.sql.push(event)
	envelope := liveEventEnvelope{
		NodeID:    event.NodeID,
		Type:      "db.query",
		Timestamp: event.Timestamp,
		SQL:       &event,
	}
	p.live.bus.publish(envelope)
	p.publishLiveClusterEvent(envelope)
}

func compactSQL(query string) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}
	parts := strings.Fields(query)
	return strings.Join(parts, " ")
}

func sanitizeLiveSQLArgs(args []interface{}) []string {
	if len(args) == 0 {
		return []string{}
	}
	limit := len(args)
	if limit > maxLiveSQLArgs {
		limit = maxLiveSQLArgs
	}
	out := make([]string, 0, limit+1)
	for _, arg := range args[:limit] {
		out = append(out, sanitizeLiveSQLArg(arg))
	}
	if len(args) > limit {
		out = append(out, fmt.Sprintf("...(+%d more)", len(args)-limit))
	}
	return out
}

func sanitizeLiveSQLArg(arg interface{}) string {
	switch v := arg.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "bool:true"
		}
		return "bool:false"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", v)
	case time.Time:
		return "time:" + v.UTC().Format(time.RFC3339)
	case []byte:
		return fmt.Sprintf("bytes(%d):***", len(v))
	case string:
		return fmt.Sprintf("string(%d):***", len(v))
	default:
		return "<redacted>"
	}
}

func (p *Panel) recordLiveSessionActivity(r *http.Request, now time.Time, traceID string) {
	if p == nil || p.live == nil || r == nil {
		return
	}

	key, token := liveSessionKey(p.config.Session, r.Context())
	if key == "" {
		return
	}

	activity := liveSessionActivity{
		NodeID:       p.liveNodeID(),
		SessionToken: token,
		TokenShort:   shortenToken(token),
		UserID:       strings.TrimSpace(observe.UserIDFromCtx(r.Context())),
		IP:           auth.ClientIPFromRequest(r),
		UserAgent:    truncateText(strings.TrimSpace(r.UserAgent()), 320),
		LastRoute:    truncateText(strings.TrimSpace(r.URL.Path), 240),
		LastSeenAt:   now.Format(time.RFC3339),
		TraceID:      strings.TrimSpace(traceID),
	}
	if activity.UserID == "" && p.config.Auth != nil {
		if user, _ := p.authenticatedUser(r); user != nil {
			activity.UserID = strings.TrimSpace(user.ID)
		}
	}
	if activity.TokenShort == "" {
		activity.TokenShort = shortenToken(key)
	}

	p.live.sessions.upsert(key, activity)
	envelope := liveEventEnvelope{
		NodeID:    activity.NodeID,
		Type:      "session.activity",
		Timestamp: activity.LastSeenAt,
		Session:   &activity,
	}
	p.live.bus.publish(envelope)
	p.publishLiveClusterEvent(envelope)
}

func liveSessionKey(sm *auth.SessionManager, ctx context.Context) (key string, token string) {
	if sm != nil && sessionContextReady(sm, ctx) {
		token = strings.TrimSpace(sm.SCS().Token(ctx))
		if token != "" {
			return "session:" + token, token
		}
	}
	reqID := strings.TrimSpace(observe.RequestIDFromCtx(ctx))
	if reqID != "" {
		return "request:" + reqID, reqID
	}
	return "", ""
}

func livePayloadPreview(r *http.Request) string {
	if r == nil {
		return ""
	}

	method := strings.ToUpper(strings.TrimSpace(r.Method))
	switch method {
	case http.MethodGet, http.MethodDelete:
		q := redactSensitiveQuery(r.URL.Query())
		encoded := q.Encode()
		if encoded == "" {
			return ""
		}
		if len(encoded) > 180 {
			encoded = encoded[:180] + "..."
		}
		return truncateText("query:"+encoded, 220)
	default:
		if r.ContentLength > 0 {
			return truncateText(fmt.Sprintf("body:redacted (%d bytes)", r.ContentLength), 220)
		}
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if ct != "" {
			return truncateText("body:redacted ("+ct+")", 220)
		}
		return "body:redacted"
	}
}

func redactSensitiveQuery(values url.Values) url.Values {
	out := url.Values{}
	for key, items := range values {
		sensitive := isSensitiveKey(key)
		for _, item := range items {
			if sensitive {
				out.Add(key, "***")
				continue
			}
			out.Add(key, item)
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	return strings.Contains(normalized, "KEY") ||
		strings.Contains(normalized, "SECRET") ||
		strings.Contains(normalized, "PASSWORD") ||
		strings.Contains(normalized, "TOKEN")
}

func truncateText(value string, maxLen int) string {
	text := strings.TrimSpace(value)
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func parseLiveListLimit(r *http.Request, fallback int) int {
	return parseLiveListLimitByKey(r, "limit", fallback)
}

func parseLiveListLimitByKey(r *http.Request, key string, fallback int) int {
	if fallback <= 0 {
		fallback = defaultLiveListLimit
	}
	if r == nil {
		return fallback
	}
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		trimmedKey = "limit"
	}
	raw := strings.TrimSpace(r.URL.Query().Get(trimmedKey))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maxLiveListLimit {
		return maxLiveListLimit
	}
	return value
}

func normalizeLiveExcludePatterns(adminPrefix string, patterns []string) []string {
	out := make([]string, 0, len(patterns)+1)
	seen := map[string]struct{}{}

	for _, pattern := range patterns {
		normalized := normalizeLiveExcludePattern(pattern)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	if len(out) == 0 {
		prefix := strings.TrimSpace(adminPrefix)
		if prefix == "" {
			prefix = "/admin"
		}
		out = append(out, prefix)
	}
	sort.Strings(out)
	return out
}

func normalizeLiveExcludePattern(pattern string) string {
	return strings.TrimSpace(pattern)
}

func (p *Panel) liveExcludePatterns() []string {
	if p == nil {
		return []string{}
	}
	p.liveExcludeMu.RLock()
	defer p.liveExcludeMu.RUnlock()

	out := make([]string, len(p.liveExcludes))
	copy(out, p.liveExcludes)
	return out
}

func (p *Panel) addLiveExcludePattern(pattern string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("admin panel not initialized")
	}
	normalized := normalizeLiveExcludePattern(pattern)
	if normalized == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if len(normalized) > 240 {
		return nil, fmt.Errorf("pattern exceeds 240 characters")
	}

	p.liveExcludeMu.Lock()
	defer p.liveExcludeMu.Unlock()
	for _, item := range p.liveExcludes {
		if item == normalized {
			out := make([]string, len(p.liveExcludes))
			copy(out, p.liveExcludes)
			return out, nil
		}
	}
	p.liveExcludes = append(p.liveExcludes, normalized)
	sort.Strings(p.liveExcludes)

	out := make([]string, len(p.liveExcludes))
	copy(out, p.liveExcludes)
	return out, nil
}

func (p *Panel) removeLiveExcludePattern(pattern string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("admin panel not initialized")
	}
	normalized := normalizeLiveExcludePattern(pattern)
	if normalized == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	p.liveExcludeMu.Lock()
	defer p.liveExcludeMu.Unlock()
	idx := -1
	for i, item := range p.liveExcludes {
		if item == normalized {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("pattern %q not found", normalized)
	}
	p.liveExcludes = append(p.liveExcludes[:idx], p.liveExcludes[idx+1:]...)
	out := make([]string, len(p.liveExcludes))
	copy(out, p.liveExcludes)
	return out, nil
}

func shouldExcludeLivePath(requestPath string, patterns []string) bool {
	pathValue := strings.TrimSpace(requestPath)
	if pathValue == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if prefix == "" || prefix == "/" {
				return true
			}
			if pathValue == prefix || strings.HasPrefix(pathValue, prefix+"/") {
				return true
			}
		}
		if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
			if matched, _ := path.Match(pattern, pathValue); matched {
				return true
			}
			continue
		}

		trimmed := strings.TrimRight(pattern, "/")
		if trimmed == "" {
			trimmed = pattern
		}
		if pathValue == pattern || pathValue == trimmed || strings.HasPrefix(pathValue, trimmed+"/") {
			return true
		}
	}
	return false
}

func (p *Panel) handleLiveSnapshot(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "live_traffic"); err != nil {
		return err
	}
	now := time.Now().UTC()
	limit := parseLiveListLimit(r, defaultLiveListLimit)
	nodeFilter := strings.TrimSpace(r.URL.Query().Get("node"))
	requestLimit := parseLiveListLimitByKey(r, "requests_limit", limit)
	sqlLimit := parseLiveListLimitByKey(r, "sql_limit", limit)
	sessionLimit := parseLiveListLimitByKey(r, "sessions_limit", limit)

	resp := liveSnapshotResponse{
		Enabled:          p != nil && p.live != nil,
		GeneratedAt:      now.Format(time.RFC3339),
		Limit:            limit,
		NodeFilter:       nodeFilter,
		TraceURLTemplate: strings.TrimSpace(p.config.TraceURLTemplate),
		RequestLimit:     requestLimit,
		SQLLimit:         sqlLimit,
		SessionLimit:     sessionLimit,
		ExcludePatterns:  []string{},
		Nodes:            []liveNodeSnapshot{},
		Requests:         []liveRequestEvent{},
		Queries:          []liveSQLEvent{},
		Sessions:         []liveSessionActivity{},
		Cluster:          p.liveClusterSnapshot(),
	}
	if p == nil || p.live == nil {
		return c.JSON(http.StatusOK, resp)
	}

	resp.ExcludePatterns = p.liveExcludePatterns()
	requestStats := p.live.requests.stats()
	sqlStats := p.live.sql.stats()
	allRequests := p.live.requests.latestFilteredByNode(requestStats.Stored, resp.ExcludePatterns, "")
	allQueries := p.live.sql.latest(sqlStats.Stored)
	allSessions := p.live.sessions.snapshot(maxLiveListLimit)
	activeNodes := p.live.nodes.active(liveNodeDegradedWindow)
	resp.Nodes = buildLiveNodeSnapshots(now, p.liveNodeID(), activeNodes, allRequests, allQueries, allSessions)
	resp.Requests = p.live.requests.latestFilteredByNode(requestLimit, resp.ExcludePatterns, nodeFilter)
	resp.Queries = filterLiveSQLByNode(p.live.sql.latest(sqlLimit), nodeFilter, sqlLimit)
	resp.Sessions = filterLiveSessionsByNode(p.live.sessions.snapshot(sessionLimit), nodeFilter, sessionLimit)
	resp.RequestBuffer = requestStats
	resp.SQLBuffer = sqlStats
	resp.Stream = p.live.bus.stats()
	return c.JSON(http.StatusOK, resp)
}

func filterLiveSQLByNode(rows []liveSQLEvent, nodeID string, limit int) []liveSQLEvent {
	if len(rows) == 0 || limit <= 0 {
		return []liveSQLEvent{}
	}
	targetNode := strings.TrimSpace(nodeID)
	if targetNode == "" {
		return rows
	}
	out := make([]liveSQLEvent, 0, len(rows))
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.NodeID), targetNode) {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func filterLiveSessionsByNode(rows []liveSessionActivity, nodeID string, limit int) []liveSessionActivity {
	if len(rows) == 0 || limit <= 0 {
		return []liveSessionActivity{}
	}
	targetNode := strings.TrimSpace(nodeID)
	if targetNode == "" {
		return rows
	}
	out := make([]liveSessionActivity, 0, len(rows))
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.NodeID), targetNode) {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildLiveNodeSnapshots(now time.Time, selfNodeID string, activeNodes map[string]time.Time, requests []liveRequestEvent, queries []liveSQLEvent, sessions []liveSessionActivity) []liveNodeSnapshot {
	type nodeAccumulator struct {
		id         string
		lastSeen   time.Time
		lastEvent  string
		requests   int
		sqlQueries int
		sessions   int
	}

	nodes := map[string]*nodeAccumulator{}
	ensure := func(raw string) *nodeAccumulator {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil
		}
		if acc, ok := nodes[id]; ok {
			return acc
		}
		acc := &nodeAccumulator{id: id}
		nodes[id] = acc
		return acc
	}

	// Bootstrap from presence registry
	for id, last := range activeNodes {
		acc := ensure(id)
		if acc != nil && last.After(acc.lastSeen) {
			acc.lastSeen = last
			acc.lastEvent = "heartbeat"
		}
	}

	mark := func(entry *nodeAccumulator, ts time.Time, eventType string) {
		if entry == nil {
			return
		}
		if ts.IsZero() {
			return
		}
		if entry.lastSeen.IsZero() || ts.After(entry.lastSeen) {
			entry.lastSeen = ts
			entry.lastEvent = eventType
		}
	}

	for _, row := range requests {
		entry := ensure(row.NodeID)
		entry.requests++
		mark(entry, parseRFC3339(row.Timestamp), "http.request")
	}
	for _, row := range queries {
		entry := ensure(row.NodeID)
		entry.sqlQueries++
		mark(entry, parseRFC3339(row.Timestamp), "db.query")
	}
	for _, row := range sessions {
		entry := ensure(row.NodeID)
		entry.sessions++
		mark(entry, parseRFC3339(row.LastSeenAt), "session.activity")
	}
	ensure(selfNodeID)

	out := make([]liveNodeSnapshot, 0, len(nodes))
	for _, node := range nodes {
		snapshot := liveNodeSnapshot{
			NodeID:        node.id,
			LastSeenAt:    formatIfSet(node.lastSeen),
			LastEventType: node.lastEvent,
			Requests:      node.requests,
			SQLQueries:    node.sqlQueries,
			Sessions:      node.sessions,
			Status:        liveNodeStatus(now, node.lastSeen),
		}
		out = append(out, snapshot)
	}

	sort.SliceStable(out, func(i, j int) bool {
		ti := parseRFC3339(out[i].LastSeenAt)
		tj := parseRFC3339(out[j].LastSeenAt)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return strings.Compare(out[i].NodeID, out[j].NodeID) < 0
	})
	return out
}

func liveNodeStatus(now, lastSeen time.Time) string {
	if lastSeen.IsZero() {
		return "idle"
	}
	age := now.Sub(lastSeen)
	if age <= liveNodeOnlineWindow {
		return "online"
	}
	if age <= liveNodeDegradedWindow {
		return "degraded"
	}
	return "stale"
}

func (p *Panel) handleListLiveExcludePatterns(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "live_traffic"); err != nil {
		return err
	}
	patterns := p.liveExcludePatterns()
	return c.JSON(http.StatusOK, map[string]interface{}{
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (p *Panel) handleAddLiveExcludePattern(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "live_traffic"); err != nil {
		return err
	}
	var payload struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	patterns, err := p.addLiveExcludePattern(payload.Pattern)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"updated":  true,
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (p *Panel) handleDeleteLiveExcludePattern(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "live_traffic"); err != nil {
		return err
	}
	pattern := c.Query("pattern")
	patterns, err := p.removeLiveExcludePattern(pattern)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"updated":  true,
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (p *Panel) handleLiveWS(c *router.Context) error {
	w, r := c.Writer, c.Request
	if err := p.authorizeAction(c, "*", "live_traffic"); err != nil {
		return err
	}
	if p == nil || p.live == nil {
		return fmt.Errorf("live runtime is not enabled")
	}
	if !allowLiveWSOrigin(r) {
		return gferrors.Forbidden("websocket origin not allowed")
	}

	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		conn.PayloadType = websocket.TextFrame

		ch, unsubscribe := p.live.bus.subscribe()
		defer unsubscribe()

		hello := liveEventEnvelope{
			Type:      "stream.ready",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := websocket.JSON.Send(conn, hello); err != nil {
			return
		}

		for event := range ch {
			if err := websocket.JSON.Send(conn, event); err != nil {
				return
			}
		}
	}).ServeHTTP(w, r)
	return nil
}

func allowLiveWSOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	originRaw := strings.TrimSpace(r.Header.Get("Origin"))
	if originRaw == "" {
		return true
	}
	origin, err := url.Parse(originRaw)
	if err != nil {
		return false
	}
	return strings.EqualFold(origin.Host, r.Host)
}
