package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

const (
	defaultSessionListLimit = 250
	maxSessionListLimit     = 2000

	sessionRealtimeBucket = 10 * time.Second
	sessionRealtimeWindow = 10 * time.Minute

	sessionHourBucket = time.Minute
	sessionHourWindow = time.Hour

	sessionDayBucket = time.Hour
)

type sessionOverviewResponse struct {
	Enabled          bool             `json:"enabled"`
	Store            string           `json:"store"`
	Reason           string           `json:"reason,omitempty"`
	GeneratedAt      string           `json:"generated_at"`
	CurrentActive    int              `json:"current_active"`
	ActiveLast5Min   int              `json:"active_last_5m"`
	ActiveLastHour   int              `json:"active_last_hour"`
	Sessions         []sessionRow     `json:"sessions"`
	Telemetry        sessionTelemetry `json:"telemetry"`
	SourceEnv        string           `json:"source_env,omitempty"`
	SourceRuntime    string           `json:"source_runtime,omitempty"`
	SourcePod        string           `json:"source_pod,omitempty"`
	SourceHost       string           `json:"source_host,omitempty"`
	SourceInstance   string           `json:"source_instance,omitempty"`
	IncludedRows     int              `json:"included_rows"`
	TruncatedByLimit bool             `json:"truncated_by_limit"`
}

// sessionRow is one row of the session viewer. The session token is a bearer
// credential — whoever holds it IS that session — so the row never serializes
// it: ID is an opaque one-way handle (see sessionHandle) that the terminate
// endpoint resolves server-side, and TokenShort is the display prefix. Any
// authenticated admin can list sessions (DatabaseAdminAuth.Authorize allows
// all actions), so serving the full token here handed every admin the means
// to replay every other admin's session.
type sessionRow struct {
	ID         string `json:"id"`
	TokenShort string `json:"token_short"`
	// User is whose session this is: the operator the panel's own
	// authentication signed in, or the identity key an application stores.
	// It is the same string the revoke-all endpoint matches on, so what an
	// operator reads in the row is exactly what "revoke every session of
	// this user" acts on.
	User string `json:"user,omitempty"`
	// UserAgent is the raw (sanitized, capped) agent the session was last
	// seen from; Device is the short label derived from it — the column
	// that lets an operator tell "that Firefox on Windows is not me".
	UserAgent string `json:"user_agent,omitempty"`
	Device    string `json:"device,omitempty"`
	// Current marks the session the request that listed them was made
	// with, so the viewer can say "this device" instead of making the
	// operator match token prefixes.
	Current     bool   `json:"current,omitempty"`
	FirstSeenAt string `json:"first_seen_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Pod         string `json:"pod,omitempty"`
	Host        string `json:"host,omitempty"`
	Instance    string `json:"instance,omitempty"`
	RemoteIP    string `json:"remote_ip,omitempty"`
	AgeSeconds  int64  `json:"age_seconds,omitempty"`
	IdleSeconds int64  `json:"idle_seconds,omitempty"`

	token     string // the raw bearer token — internal only, never serialized
	firstSeen time.Time
	lastSeen  time.Time
	expiresAt time.Time
}

// sessionHandle derives the opaque per-session id served in sessionRow.ID:
// a truncated SHA-256 of the token. One-way (the token cannot be recovered
// from it) and stable, so the SPA can key rows and address the terminate
// endpoint without ever seeing the credential.
func sessionHandle(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

type sessionTelemetry struct {
	Realtime sessionSeries `json:"realtime"`
	LastHour sessionSeries `json:"last_hour"`
	Today    sessionSeries `json:"today"`
}

type sessionSeries struct {
	Label         string               `json:"label"`
	BucketSeconds int                  `json:"bucket_seconds"`
	Points        []sessionSeriesPoint `json:"points"`
}

type sessionSeriesPoint struct {
	Timestamp string `json:"timestamp"`
	Active    int    `json:"active"`
}

type iterableSessionStore interface {
	All() (map[string][]byte, error)
}

type iterableSessionStoreCtx interface {
	AllCtx(context.Context) (map[string][]byte, error)
}

func (p *Panel) handleListSessions(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "list_sessions"); err != nil {
		return err
	}

	now := time.Now().UTC()
	limit := parseSessionListLimit(r, defaultSessionListLimit)

	resp := sessionOverviewResponse{
		Enabled:     false,
		Store:       normalizeSessionStoreLabel(p.config.SessionStore),
		GeneratedAt: now.Format(time.RFC3339),
		Telemetry: sessionTelemetry{
			Realtime: sessionSeries{
				Label:         "real_time",
				BucketSeconds: int(sessionRealtimeBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
			LastHour: sessionSeries{
				Label:         "last_hour",
				BucketSeconds: int(sessionHourBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
			Today: sessionSeries{
				Label:         "today",
				BucketSeconds: int(sessionDayBucket.Seconds()),
				Points:        []sessionSeriesPoint{},
			},
		},
	}

	if p.config.Session == nil {
		resp.Reason = "session manager is not configured in admin panel"
		return c.JSON(http.StatusOK, resp)
	}

	resp.Enabled = true
	resp.SourceEnv = strings.TrimSpace(p.config.Environment)
	resp.SourceRuntime = classifyRuntime(p.config.SessionRuntime)
	resp.SourcePod = strings.TrimSpace(p.config.SessionRuntime.Pod)
	resp.SourceHost = strings.TrimSpace(p.config.SessionRuntime.Host)
	if resp.SourcePod != "" && strings.EqualFold(resp.SourcePod, resp.SourceHost) {
		resp.SourcePod = ""
	}
	resp.SourceInstance = p.config.SessionRuntime.Instance

	rawSessions, supported, err := allSessionPayloads(r.Context(), p.config.Session)
	if err != nil {
		return fmt.Errorf("admin.ListSessions load: %w", err)
	}
	if !supported {
		resp.Enabled = false
		resp.Reason = "session store does not support listing active sessions"
		return c.JSON(http.StatusOK, resp)
	}

	current := p.currentSessionToken(r.Context())
	rows := make([]sessionRow, 0, len(rawSessions))
	for token, payload := range rawSessions {
		deadline, values, err := p.config.Session.SCS().Codec.Decode(payload)
		if err != nil {
			continue
		}

		row := buildSessionRow(token, deadline, values, now)
		row.Current = current != "" && token == current
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].lastSeen.Equal(rows[j].lastSeen) {
			return rows[i].lastSeen.After(rows[j].lastSeen)
		}
		if !rows[i].expiresAt.Equal(rows[j].expiresAt) {
			return rows[i].expiresAt.After(rows[j].expiresAt)
		}
		return rows[i].token < rows[j].token
	})

	resp.CurrentActive = len(rows)
	for _, row := range rows {
		if !row.lastSeen.IsZero() {
			if now.Sub(row.lastSeen) <= 5*time.Minute {
				resp.ActiveLast5Min++
			}
			if now.Sub(row.lastSeen) <= sessionHourWindow {
				resp.ActiveLastHour++
			}
		}
	}

	if len(rows) > limit {
		resp.TruncatedByLimit = true
		rows = rows[:limit]
	}
	resp.IncludedRows = len(rows)
	resp.Sessions = rows

	resp.Telemetry.Realtime.Points = buildSessionSeries(rows, now.Add(-sessionRealtimeWindow), now, sessionRealtimeBucket)
	resp.Telemetry.LastHour.Points = buildSessionSeries(rows, now.Add(-sessionHourWindow), now, sessionHourBucket)
	resp.Telemetry.Today.Points = buildTodaySeries(rows, now, sessionDayBucket)

	return c.JSON(http.StatusOK, resp)
}

func buildSessionRow(token string, deadline time.Time, values map[string]interface{}, now time.Time) sessionRow {
	firstSeen := parseSessionMetaTime(valueAsString(values, auth.SessionMetaFirstSeenAtKey))
	lastSeen := parseSessionMetaTime(valueAsString(values, auth.SessionMetaLastSeenAtKey))
	if firstSeen.IsZero() {
		firstSeen = lastSeen
	}
	if lastSeen.IsZero() {
		lastSeen = firstSeen
	}
	if firstSeen.IsZero() {
		firstSeen = now
	}

	expiresAt := deadline.UTC()
	if expiresAt.IsZero() {
		expiresAt = now
	}

	row := sessionRow{
		ID:         sessionHandle(token),
		token:      token,
		TokenShort: shortenToken(token),
		User:       detectSessionUser(values),
		UserAgent:  valueAsString(values, auth.SessionMetaUserAgentKey),
		ExpiresAt:  formatIfSet(expiresAt),
		Pod:        valueAsString(values, auth.SessionMetaPodKey),
		Host:       valueAsString(values, auth.SessionMetaHostKey),
		Instance:   valueAsString(values, auth.SessionMetaInstanceKey),
		RemoteIP:   valueAsString(values, auth.SessionMetaRemoteIPKey),
		firstSeen:  firstSeen,
		lastSeen:   lastSeen,
		expiresAt:  expiresAt,
	}
	if row.Pod != "" && strings.EqualFold(row.Pod, row.Host) {
		row.Pod = ""
	}
	row.Device = describeDevice(row.UserAgent)

	if !firstSeen.IsZero() {
		row.FirstSeenAt = firstSeen.Format(time.RFC3339)
		row.AgeSeconds = int64(now.Sub(firstSeen).Seconds())
	}
	if !lastSeen.IsZero() {
		row.LastSeenAt = lastSeen.Format(time.RFC3339)
		row.IdleSeconds = int64(now.Sub(lastSeen).Seconds())
	}

	return row
}

func parseSessionListLimit(r *http.Request, fallback int) int {
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maxSessionListLimit {
		return maxSessionListLimit
	}
	return value
}

func buildSessionSeries(rows []sessionRow, start, end time.Time, bucket time.Duration) []sessionSeriesPoint {
	if bucket <= 0 {
		return []sessionSeriesPoint{}
	}
	if end.Before(start) {
		start, end = end, start
	}

	points := make([]sessionSeriesPoint, 0, int(end.Sub(start)/bucket)+1)
	for ts := start; !ts.After(end); ts = ts.Add(bucket) {
		active := 0
		for _, row := range rows {
			if isSessionActiveAt(row, ts) {
				active++
			}
		}
		points = append(points, sessionSeriesPoint{
			Timestamp: ts.Format(time.RFC3339),
			Active:    active,
		})
	}
	return points
}

func buildTodaySeries(rows []sessionRow, now time.Time, bucket time.Duration) []sessionSeriesPoint {
	loc := now.Location()
	startLocal := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	return buildSessionSeries(rows, startLocal, now.In(loc), bucket)
}

func isSessionActiveAt(row sessionRow, ts time.Time) bool {
	start := row.firstSeen
	if start.IsZero() {
		start = row.lastSeen
	}
	if start.IsZero() {
		start = ts
	}
	end := row.expiresAt
	if end.IsZero() {
		end = ts.Add(time.Second)
	}

	return !start.After(ts) && end.After(ts)
}

// handleTerminateSession destroys one session addressed by the opaque handle
// the session list serves (sessionRow.ID) — the backend of the session list's
// "terminate" action. The handle is resolved to the real token server-side,
// so revocation works without the list ever serving the bearer credential.
// A raw token is still accepted for compatibility (a caller that has the
// token holds the credential already — accepting it reveals nothing new).
func (p *Panel) handleTerminateSession(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "terminate_sessions"); err != nil {
		return err
	}

	if p.config.Session == nil {
		return gferrors.BadRequest("session manager is not configured in admin panel")
	}
	handle := strings.TrimSpace(c.Param("token"))
	if handle == "" {
		return gferrors.BadRequest("session id is required")
	}

	store := p.config.Session.SCS().Store

	// Resolve the opaque handle to the stored token.
	token, err := p.resolveSessionHandle(r.Context(), handle)
	if err != nil {
		return fmt.Errorf("admin.TerminateSession resolve: %w", err)
	}

	// Fallback: the parameter may be a raw token (pre-handle clients).
	found := token != ""
	if !found {
		token = handle
		if finder, ok := store.(interface {
			FindCtx(context.Context, string) ([]byte, bool, error)
		}); ok {
			_, found, err = finder.FindCtx(r.Context(), token)
		} else {
			_, found, err = store.Find(token)
		}
		if err != nil {
			return fmt.Errorf("admin.TerminateSession find: %w", err)
		}
	}
	if !found {
		return gferrors.NotFound("session", shortenToken(handle))
	}

	if deleter, ok := store.(interface {
		DeleteCtx(context.Context, string) error
	}); ok {
		err = deleter.DeleteCtx(r.Context(), token)
	} else {
		err = store.Delete(token)
	}
	if err != nil {
		return fmt.Errorf("admin.TerminateSession delete: %w", err)
	}

	// Audited explicitly like every other mutation (never the full token —
	// it is a bearer credential).
	p.recordAuditEntry(r, AuditEntry{
		Action:   "session.terminate",
		RecordID: shortenToken(token),
	})

	return c.JSON(http.StatusOK, map[string]any{
		"terminated":  true,
		"id":          sessionHandle(token),
		"token_short": shortenToken(token),
	})
}

// resolveSessionHandle maps an opaque session handle (sessionRow.ID) back to
// the stored token by scanning the active sessions. Returns "" when no active
// session matches — including when the store cannot enumerate sessions, in
// which case only the raw-token fallback can address one.
func (p *Panel) resolveSessionHandle(ctx context.Context, handle string) (string, error) {
	payloads, supported, err := allSessionPayloads(ctx, p.config.Session)
	if err != nil {
		return "", err
	}
	if !supported {
		return "", nil
	}
	for token := range payloads {
		if sessionHandle(token) == handle {
			return token, nil
		}
	}
	return "", nil
}

func allSessionPayloads(ctx context.Context, sm *auth.SessionManager) (map[string][]byte, bool, error) {
	store := sm.SCS().Store

	if withCtx, ok := store.(iterableSessionStoreCtx); ok {
		all, err := withCtx.AllCtx(ctx)
		if err != nil {
			return nil, true, err
		}
		return all, true, nil
	}
	if plain, ok := store.(iterableSessionStore); ok {
		all, err := plain.All()
		if err != nil {
			return nil, true, err
		}
		return all, true, nil
	}
	return nil, false, nil
}

func parseSessionMetaTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

func valueAsString(values map[string]interface{}, key string) string {
	if len(values) == 0 || key == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// detectSessionUser names whose session a payload is. The panel's own
// authentication provider stores the operator under its private keys, and
// those come first: a panel that reads only the generic keys an application
// might use lists its own operators as nobody — which is what the viewer did
// until A6, so every revocation from it was done blind (OR-46).
func detectSessionUser(values map[string]interface{}) string {
	candidates := []string{
		adminSessionUsernameKey,
		adminSessionEmailKey,
		adminSessionUserIDKey,
		"user_email",
		"email",
		"username",
		"user_name",
		"user_id",
		"uid",
		"id",
	}
	for _, key := range candidates {
		if v := valueAsString(values, key); v != "" {
			return v
		}
	}
	return ""
}

func shortenToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		return token
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func formatIfSet(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func normalizeSessionStoreLabel(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "memory"
	}
	return value
}

func classifyRuntime(identity auth.SessionRuntimeIdentity) string {
	if strings.TrimSpace(identity.Pod) != "" {
		return "kubernetes"
	}
	return "standalone"
}

// currentSessionToken returns the token of the session the request was made
// with, or "" when the session middleware did not run for this request (a
// panel wired without it, as some tests do) or the session is not committed
// yet. It is what lets a list mark "this one is you" and what a bulk
// revocation keeps: the request that revokes never revokes itself.
func (p *Panel) currentSessionToken(ctx context.Context) string {
	if p == nil || p.config.Session == nil || !sessionContextReady(p.config.Session, ctx) {
		return ""
	}
	return strings.TrimSpace(p.config.Session.Token(ctx))
}

// maxStoredUserAgent mirrors the framework's cap: a user agent is
// attacker-controlled text that lands in a session payload, an operator's
// screen and possibly a log line, so it is truncated rather than trusted.
const maxStoredUserAgent = 256

// sanitizeUserAgent applies the same rules the framework's session runtime
// middleware applies before storing an agent: control characters out,
// length capped. The panel records it itself because its activity
// middleware refreshes the session on every panel request and the
// framework's only every thirty seconds — a device list fed by the
// framework alone would name the device of a session that just signed in
// and nothing about one that has been open all morning.
func sanitizeUserAgent(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if len(cleaned) > maxStoredUserAgent {
		cleaned = cleaned[:maxStoredUserAgent]
	}
	return cleaned
}

// describeDevice turns a user agent into the short label a session row
// shows: the browser and the platform, which is what an operator compares
// against the devices they know they hold. It is deliberately a small
// classifier — the common browsers, the common platforms, and the first
// product token for everything else (a script announces itself as
// "Go-http-client" or "curl", and that IS the useful answer). The raw
// agent travels next to it for the cases the label cannot settle.
func describeDevice(userAgent string) string {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)

	browser := ""
	switch {
	case strings.Contains(lower, "edg/") || strings.Contains(lower, "edge/"):
		browser = "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox/") || strings.Contains(lower, "fxios/"):
		browser = "Firefox"
	case strings.Contains(lower, "crios/"):
		browser = "Chrome"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "chromium/"):
		browser = "Chrome"
	case strings.Contains(lower, "safari/") && strings.Contains(lower, "version/"):
		browser = "Safari"
	}

	platform := ""
	switch {
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ipod"):
		platform = "iOS"
	case strings.Contains(lower, "android"):
		platform = "Android"
	case strings.Contains(lower, "windows"):
		platform = "Windows"
	case strings.Contains(lower, "cros "):
		platform = "ChromeOS"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os x"):
		platform = "macOS"
	case strings.Contains(lower, "linux"):
		platform = "Linux"
	}

	if browser == "" {
		// Not a browser the classifier knows: the first product token
		// (the part before "/" or the first space) is the honest name —
		// "Go-http-client", "curl", "PostmanRuntime".
		token := ua
		if i := strings.IndexAny(token, "/ ("); i > 0 {
			token = token[:i]
		}
		browser = strings.TrimSpace(token)
	}
	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case browser != "":
		return browser
	default:
		return platform
	}
}

// handleRevokeUserSessions ends every session of one user — the button an
// incident needs, backed by the framework's RevokeWhere. The user is the
// same string the session list shows in its `user` column, so the operator
// acts on exactly what they read. The session the request is made with is
// kept even when it matches: a request that revoked itself would sign the
// operator out mid-action, and "sign out everywhere else" is the operation
// people actually mean. The response says how many were ended and whether
// the caller's own was among the matches and kept.
func (p *Panel) handleRevokeUserSessions(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "terminate_sessions"); err != nil {
		return err
	}
	if p.config.Session == nil {
		return gferrors.BadRequest("session manager is not configured in admin panel")
	}

	var req struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	user := strings.TrimSpace(req.User)
	if user == "" {
		return gferrors.BadRequest("user is required")
	}

	current := p.currentSessionToken(r.Context())
	keptCurrent := false
	revoked, err := p.config.Session.RevokeWhere(r.Context(), func(info auth.SessionInfo) bool {
		if detectSessionUser(info.Values) != user {
			return false
		}
		if current != "" && info.Token == current {
			keptCurrent = true
			return false
		}
		return true
	})
	if errors.Is(err, auth.ErrSessionStoreNotIterable) {
		return gferrors.BadRequest("session store does not support listing active sessions")
	}

	// Audited whether it completed or not: a partial revocation is exactly
	// the outcome an operator retrying needs to see in the trail. The
	// record is the user, never a token.
	p.recordAuditEntry(r, AuditEntry{
		Action:   "session.revoke_all",
		RecordID: user,
		NewValue: map[string]any{
			"revoked":      revoked,
			"kept_current": keptCurrent,
			"completed":    err == nil,
		},
	})
	if err != nil {
		return fmt.Errorf("admin.RevokeUserSessions: %w", err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"user":         user,
		"revoked":      revoked,
		"kept_current": keptCurrent,
	})
}
