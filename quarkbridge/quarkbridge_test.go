package quarkbridge

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/quark"
)

// errDriver is a minimal database/sql driver whose queries always fail, so a
// test can obtain a real *sql.Row carrying an error without pulling in a full
// SQL engine. Registered once in init.
type errDriver struct{}

func (errDriver) Open(string) (driver.Conn, error) { return errConn{}, nil }

type errConn struct{}

func (errConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (errConn) Close() error                        { return nil }
func (errConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (errConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("query boom")
}

func init() { sql.Register("quarkbridge_errdriver", errDriver{}) }

// fakeSink captures published events so a test can assert on them without a
// live observability bus.
type fakeSink struct{ events []nucleus.SQLEvent }

func (f *fakeSink) EmitSQL(ev nucleus.SQLEvent) { f.events = append(f.events, ev) }

// The Middleware must satisfy Quark's interface and, via SQLSink, accept a real
// nucleus.EventBus.
var (
	_ quark.Middleware = (*Middleware)(nil)
	_ SQLSink          = (nucleus.EventBus)(nil)
)

func ctxWithCorrelation(reqID, traceID, userID string) context.Context {
	ctx := observe.CtxWithRequestID(context.Background(), reqID)
	ctx = observe.CtxWithTraceID(ctx, traceID)
	ctx = observe.CtxWithUserID(ctx, userID)
	return ctx
}

// WrapExec times the call, derives the operation, masks string args by default,
// and fills the correlation fields from ctx.
func TestWrapExec_EmitsCorrelatedRedactedEvent(t *testing.T) {
	sink := &fakeSink{}
	m := New(sink, WithNodeID(" node-x "))
	ctx := ctxWithCorrelation("req-1", "trace-1", "user-1")

	next := func(context.Context, quark.Executor, string, []any) (sql.Result, error) {
		return nil, nil
	}
	wrapped := m.WrapExec(next)
	_, _ = wrapped(ctx, nil, "INSERT INTO users (name, age)\n  VALUES (?, ?)", []any{"alice", 30})

	if len(sink.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Operation != "INSERT" {
		t.Errorf("Operation = %q, want INSERT", ev.Operation)
	}
	if ev.Query != "INSERT INTO users (name, age) VALUES (?, ?)" {
		t.Errorf("Query not compacted: %q", ev.Query)
	}
	if ev.NodeID != "node-x" {
		t.Errorf("NodeID = %q, want node-x (trimmed)", ev.NodeID)
	}
	if ev.RequestID != "req-1" || ev.TraceID != "trace-1" || ev.UserID != "user-1" {
		t.Errorf("correlation wrong: req=%q trace=%q user=%q", ev.RequestID, ev.TraceID, ev.UserID)
	}
	// Default redaction: the string is masked, the number kept verbatim.
	if len(ev.Args) != 2 || ev.Args[0] != "string(5):***" || ev.Args[1] != "30" {
		t.Errorf("Args = %v, want [string(5):*** 30]", ev.Args)
	}
	if ev.Err != "" {
		t.Errorf("Err = %q, want empty", ev.Err)
	}
}

// WrapQuery captures the query error and does not mask numeric keys.
func TestWrapQuery_CapturesError(t *testing.T) {
	sink := &fakeSink{}
	m := New(sink)
	next := func(context.Context, quark.Executor, string, []any) (*sql.Rows, error) {
		return nil, errors.New("boom")
	}
	wrapped := m.WrapQuery(next)
	_, err := wrapped(context.Background(), nil, "SELECT * FROM widgets WHERE id = ?", []any{42})
	if err == nil {
		t.Fatal("wrapped WrapQuery should propagate the underlying error")
	}
	if len(sink.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Operation != "SELECT" {
		t.Errorf("Operation = %q, want SELECT", ev.Operation)
	}
	if ev.Err != "boom" {
		t.Errorf("Err = %q, want boom", ev.Err)
	}
	if len(ev.Args) != 1 || ev.Args[0] != "42" {
		t.Errorf("Args = %v, want [42] (numeric kept verbatim)", ev.Args)
	}
}

// IncludeArgs opts out of masking and shows raw values.
func TestWrapExec_IncludeArgsShowsRawValues(t *testing.T) {
	sink := &fakeSink{}
	m := New(sink, WithRedaction(IncludeArgs))
	next := func(context.Context, quark.Executor, string, []any) (sql.Result, error) {
		return nil, nil
	}
	wrapped := m.WrapExec(next)
	_, _ = wrapped(context.Background(), nil, "UPDATE users SET email = ? WHERE id = ?", []any{"a@b.com", 7})

	ev := sink.events[0]
	if len(ev.Args) != 2 || ev.Args[0] != "a@b.com" || ev.Args[1] != "7" {
		t.Errorf("Args = %v, want [a@b.com 7]", ev.Args)
	}
}

// WrapQueryRow reads the error from a real (*sql.Row).Err.
func TestWrapQueryRow_RealRowError(t *testing.T) {
	db, err := sql.Open("quarkbridge_errdriver", "")
	if err != nil {
		t.Fatalf("open fake driver: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sink := &fakeSink{}
	m := New(sink)
	next := func(ctx context.Context, _ quark.Executor, s string, args []any) *sql.Row {
		return db.QueryRowContext(ctx, s, args...)
	}
	wrapped := m.WrapQueryRow(next)
	row := wrapped(context.Background(), nil, "SELECT id FROM t WHERE id = ?", []any{1})
	_ = row.Scan(new(int)) // force evaluation; the error was already captured at publish

	if len(sink.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(sink.events))
	}
	if sink.events[0].Err == "" {
		t.Error("expected a non-empty Err from the failing query")
	}
	if sink.events[0].Operation != "SELECT" {
		t.Errorf("Operation = %q, want SELECT", sink.events[0].Operation)
	}
}

// A nil sink is a safe no-op pass-through.
func TestNilSink_NoPanic(t *testing.T) {
	m := New(nil)
	next := func(context.Context, quark.Executor, string, []any) (sql.Result, error) {
		return nil, nil
	}
	if _, err := m.WrapExec(next)(context.Background(), nil, "DELETE FROM t", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Duration is measured across next using the injectable clock.
func TestDuration_MeasuredAcrossNext(t *testing.T) {
	sink := &fakeSink{}
	m := New(sink)
	// Deterministic clock: first call = start, second = end (+2ms).
	base := time.Unix(1700000000, 0).UTC()
	calls := 0
	m.now = func() time.Time {
		calls++
		if calls == 1 {
			return base
		}
		return base.Add(2 * time.Millisecond)
	}
	next := func(context.Context, quark.Executor, string, []any) (sql.Result, error) {
		return nil, nil
	}
	_, _ = m.WrapExec(next)(context.Background(), nil, "SELECT 1", nil)

	ev := sink.events[0]
	if ev.Duration != 2*time.Millisecond {
		t.Errorf("Duration = %v, want 2ms", ev.Duration)
	}
	if !ev.EmittedAt.Equal(base.Add(2 * time.Millisecond)) {
		t.Errorf("EmittedAt = %v, want end time", ev.EmittedAt)
	}
}

// Args over the cap are summarised rather than shipped in full.
func TestRenderArgs_CapsCount(t *testing.T) {
	sink := &fakeSink{}
	m := New(sink, WithRedaction(IncludeArgs))
	args := make([]any, maxArgs+3)
	for i := range args {
		args[i] = i
	}
	next := func(context.Context, quark.Executor, string, []any) (sql.Result, error) {
		return nil, nil
	}
	_, _ = m.WrapExec(next)(context.Background(), nil, "SELECT 1", args)

	got := sink.events[0].Args
	if len(got) != maxArgs+1 {
		t.Fatalf("want %d rendered args (cap + summary), got %d", maxArgs+1, len(got))
	}
	if got[maxArgs] != "...(+3 more)" {
		t.Errorf("summary = %q, want ...(+3 more)", got[maxArgs])
	}
}
