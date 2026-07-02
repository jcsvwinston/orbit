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
	sink      SQLSink
	nodeID    string
	redaction RedactionMode
	now       func() time.Time
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

// publish maps one executed statement to a nucleus.SQLEvent and emits it. The
// ModelName field is left empty: the model/table name is known to Quark's
// QueryObserver but not to a Middleware, which sees only the rendered SQL.
// Operation is therefore derived from the leading SQL keyword.
func (m *Middleware) publish(ctx context.Context, start time.Time, sqlStr string, args []any, execErr error) {
	if m.sink == nil {
		return
	}
	end := m.now()
	ev := nucleus.SQLEvent{
		EmittedAt: end,
		NodeID:    m.nodeID,
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
