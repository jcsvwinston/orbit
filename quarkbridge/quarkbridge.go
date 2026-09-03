// Package quarkbridge publishes the SQL statements a Quark ORM client executes
// onto a Nucleus observability feed, so they show up in Orbit's live SQL view
// correlated to the originating HTTP request.
//
// It is an opt-in Quark Middleware. Wire it into a *quark.Client and point it
// at the Nucleus event bus (rt.Observability(), which returns a nucleus.EventBus):
//
//	bridge := quarkbridge.New(rt.Observability())
//	client, err := quark.New("pgx", dsn, quark.WithMiddleware(bridge))
//
// Every statement the client runs is then timed, mapped to a nucleus.SQLEvent,
// and published through the framework's public SQL ingest (EmitSQL, ADR-020) —
// the same feed Orbit already drains via SubscribeSQL. No change to Orbit and
// no change to either product core is required (QADR-0006): the bridge depends
// on both Quark and Nucleus and lives outside their cores.
//
// # Correlation
//
// RequestID, TraceID and UserID are read from the ctx that Quark threads
// through the middleware, using Nucleus's own context helpers. That is why the
// bridge is a Middleware (which receives ctx) rather than a QueryObserver
// (which does not) — without ctx the feed would lose the link to the request.
//
// # Model names
//
// Quark has no model registry the bridge could consult, and a Middleware sees
// only the rendered SQL, so ModelName is derived from the statement's primary
// table (FROM/INTO/UPDATE/DELETE FROM). That is the TABLE name, which is what
// the fleet's sql_models filter then matches for bridged statements; map
// tables to your own model names with WithModelNames when they differ.
// Statements without a recognisable table (DDL, CTE-first queries) carry an
// empty ModelName, exactly as before.
//
// # Redaction
//
// By default bind arguments are masked exactly the way Nucleus masks its own
// SQL feed: string and []byte values become "type(len):***" markers, while
// numeric, bool, time.Time and nil values are kept verbatim (so a "WHERE id = ?"
// key still reads as e.g. "42"). Opt into raw values with
// WithRedaction(IncludeArgs) for local debugging only — it applies no scrubbing.
//
// # OTel
//
// OpenTelemetry (quark/otel) is complementary, not the transport for this feed:
// its spans are exported in batch for durable tracing and would not be real
// time. This bridge is the live-feed path; run both if you want durable traces
// too, sharing the same tracer so Quark's spans nest under the request span.
package quarkbridge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/quark"
)

// maxArgs bounds how many bound arguments a published event carries, mirroring
// Nucleus's own SQL observer. Extra args are summarised as "...(+N more)".
const maxArgs = 16

// SQLSink is the minimal ingest the bridge publishes to. A nucleus.EventBus
// (returned by nucleus.Runtime.Observability()) satisfies it via EmitSQL, so a
// caller passes that value directly; tests can pass a lightweight fake.
type SQLSink interface {
	EmitSQL(nucleus.SQLEvent)
}

// RedactionMode controls whether bind arguments are exposed on published
// events. It mirrors the redaction principle Quark applies to its OTel spans.
type RedactionMode int

const (
	// RedactArgs is the default. String and []byte argument values are masked
	// as "type(len):***"; numeric, bool, time.Time and nil values are kept
	// verbatim — the same convention Nucleus uses for its own SQL feed, so
	// bridged statements render consistently alongside framework ones.
	RedactArgs RedactionMode = iota

	// IncludeArgs places raw argument values on the event via fmt.Sprintf("%v",
	// arg), with no scrubbing. Opt in only for local debugging.
	IncludeArgs
)

// Middleware is a quark.Middleware that publishes executed SQL to a Nucleus
// feed. Construct it with New and pass it to quark.WithMiddleware. It is safe
// for concurrent use: it holds only immutable configuration.
type Middleware struct {
	sink       SQLSink
	nodeID     string
	redaction  RedactionMode
	modelNames map[string]string
	now        func() time.Time
}

// Option configures a Middleware.
type Option func(*Middleware)

// WithNodeID tags published events with the framework process identifier. It
// matches the NodeID Nucleus's own observer stamps; leave it unset for local
// development (NodeID may be empty).
func WithNodeID(id string) Option {
	return func(m *Middleware) { m.nodeID = strings.TrimSpace(id) }
}

// WithRedaction sets how bind arguments are exposed. The default is RedactArgs.
func WithRedaction(mode RedactionMode) Option {
	return func(m *Middleware) { m.redaction = mode }
}

// WithModelNames maps table names (as they appear in SQL, matched
// case-insensitively) to the model names published on events. Tables absent
// from the map publish their table name.
func WithModelNames(tableToModel map[string]string) Option {
	return func(m *Middleware) {
		m.modelNames = make(map[string]string, len(tableToModel))
		for table, model := range tableToModel {
			m.modelNames[strings.ToLower(strings.TrimSpace(table))] = strings.TrimSpace(model)
		}
	}
}

// New returns a Middleware that publishes to sink. A nil sink makes every
// wrapped call a straight pass-through (the bridge emits nothing), so wiring
// the bridge without a live feed is harmless.
func New(sink SQLSink, opts ...Option) *Middleware {
	m := &Middleware{
		sink: sink,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// WrapExec implements quark.Middleware. It times the execution and publishes
// the statement (with any error) after next returns.
func (m *Middleware) WrapExec(next quark.ExecFunc) quark.ExecFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) (sql.Result, error) {
		start := m.now()
		res, err := next(ctx, exec, sqlStr, args)
		m.publish(ctx, start, sqlStr, args, err)
		return res, err
	}
}

// WrapQuery implements quark.Middleware for row-returning queries.
func (m *Middleware) WrapQuery(next quark.QueryFunc) quark.QueryFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) (*sql.Rows, error) {
		start := m.now()
		rows, err := next(ctx, exec, sqlStr, args)
		m.publish(ctx, start, sqlStr, args, err)
		return rows, err
	}
}

// WrapQueryRow implements quark.Middleware for single-row queries. The error is
// read from (*sql.Row).Err, which reports a failure of the underlying query
// before Scan is called.
func (m *Middleware) WrapQueryRow(next quark.QueryRowFunc) quark.QueryRowFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) *sql.Row {
		start := m.now()
		row := next(ctx, exec, sqlStr, args)
		var err error
		if row != nil {
			err = row.Err()
		}
		m.publish(ctx, start, sqlStr, args, err)
		return row
	}
}

// publish maps one executed statement to a nucleus.SQLEvent and emits it.
// A Middleware sees only the rendered SQL (Quark's QueryObserver knows the
// table but receives no ctx), so both Operation and ModelName are derived
// from the statement: the leading keyword and the primary table.
func (m *Middleware) publish(ctx context.Context, start time.Time, sqlStr string, args []any, execErr error) {
	if m.sink == nil {
		return
	}
	end := m.now()
	ev := nucleus.SQLEvent{
		EmittedAt: end,
		NodeID:    m.nodeID,
		ModelName: m.modelName(sqlStr),
		Operation: operationOf(sqlStr),
		Query:     compact(sqlStr),
		Args:      m.renderArgs(args),
		Duration:  end.Sub(start),
		RequestID: observe.RequestIDFromCtx(ctx),
		TraceID:   observe.TraceIDFromCtx(ctx),
		UserID:    observe.UserIDFromCtx(ctx),
	}
	if execErr != nil {
		ev.Err = execErr.Error()
	}
	m.sink.EmitSQL(ev)
}

// modelName resolves the statement's primary table to a model name.
func (m *Middleware) modelName(sqlStr string) string {
	table := tableOf(sqlStr)
	if table == "" {
		return ""
	}
	if mapped, ok := m.modelNames[strings.ToLower(table)]; ok && mapped != "" {
		return mapped
	}
	return table
}

// tableOf returns the primary table of a DML statement: the identifier after
// the first FROM (SELECT, DELETE), INTO (INSERT/REPLACE) or UPDATE keyword.
// Quoting and schema qualification are stripped ("public"."users" → users).
// Returns "" when no such keyword names an identifier.
func tableOf(sqlStr string) string {
	tokens := strings.Fields(sqlStr)
	if len(tokens) == 0 {
		return ""
	}
	want := ""
	switch strings.ToUpper(tokens[0]) {
	case "SELECT", "DELETE":
		want = "FROM"
	case "INSERT", "REPLACE":
		want = "INTO"
	case "UPDATE":
		want = "UPDATE"
	default:
		return ""
	}
	for i, tok := range tokens {
		if !strings.EqualFold(tok, want) || i+1 >= len(tokens) {
			continue
		}
		return cleanIdentifier(tokens[i+1])
	}
	return ""
}

// cleanIdentifier strips quoting, a trailing "(" or "," and the schema
// prefix from a SQL identifier token. It returns "" for a token that starts a
// subquery or is not an identifier.
func cleanIdentifier(tok string) string {
	tok = strings.TrimRight(tok, ",;")
	if i := strings.IndexByte(tok, '('); i >= 0 {
		tok = tok[:i]
	}
	if tok == "" {
		return ""
	}
	if i := strings.LastIndexByte(tok, '.'); i >= 0 {
		tok = tok[i+1:]
	}
	tok = strings.Trim(tok, "`\"[]")
	if tok == "" || !isIdentifier(tok) {
		return ""
	}
	return tok
}

func isIdentifier(s string) bool {
	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// operationOf returns the upper-cased leading keyword of the statement
// (SELECT/INSERT/UPDATE/DELETE/WITH/…), or "" for an empty statement.
func operationOf(sqlStr string) string {
	for _, tok := range strings.Fields(sqlStr) {
		return strings.ToUpper(tok)
	}
	return ""
}

// compact collapses runs of whitespace to single spaces so multi-line SQL reads
// as one line in the feed.
func compact(sqlStr string) string {
	return strings.Join(strings.Fields(sqlStr), " ")
}

func (m *Middleware) renderArgs(args []any) []string {
	if len(args) == 0 {
		return nil
	}
	limit := len(args)
	if limit > maxArgs {
		limit = maxArgs
	}
	out := make([]string, 0, limit+1)
	for _, a := range args[:limit] {
		out = append(out, m.renderArg(a))
	}
	if len(args) > limit {
		out = append(out, fmt.Sprintf("...(+%d more)", len(args)-limit))
	}
	return out
}

// renderArg formats one bound argument per the configured RedactionMode.
func (m *Middleware) renderArg(a any) string {
	if m.redaction == IncludeArgs {
		return fmt.Sprintf("%v", a)
	}
	// RedactArgs (default): mask sensitive values, keep primitives — the same
	// convention Nucleus's own SQL observer applies, for a consistent feed.
	switch v := a.(type) {
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

// compile-time assertion that Middleware satisfies the Quark contract.
var _ quark.Middleware = (*Middleware)(nil)
