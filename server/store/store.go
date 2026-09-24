// Package store is the admin server's local retention: the events it
// replays, the fleet audit trail and the host-metrics samples per node,
// kept in one SQLite file under Config.DataDir for a retention window
// (ADR-013). Without a data directory the server keeps what it always
// kept — bounded in-memory rings that a restart empties — and this
// package is not used.
//
// The store is a single writer behind a channel: every append is queued
// and written in batches by one goroutine, so a burst of events costs the
// receiving path a channel send, not a transaction. Reads go to SQLite
// directly and are bounded by the window: a row older than the window is
// not returned even before the janitor deletes it.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	// The pure-Go driver, registered under "sqlite": the admin server
	// binary builds without cgo.
	_ "modernc.org/sqlite"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// FileName is the SQLite file the store opens under the data directory.
const FileName = "fleet.db"

// AuditEntry is one fleet-plane action as the store keeps it. It mirrors
// routing.AuditEntry without importing it: the store is below routing.
type AuditEntry struct {
	Time   time.Time
	Actor  string
	Action string
	Target string
	NodeID string
	Before string
	After  string
}

// HostMetricsSample is one heartbeat's host metrics, stamped with the
// time the server received it.
type HostMetricsSample struct {
	Time    time.Time
	NodeID  string
	Metrics *adminv1.HostMetrics
}

// Store is the retention store. Open it with Open; Close flushes what is
// queued and closes the file.
type Store struct {
	db        *sql.DB
	retention time.Duration
	now       func() time.Time

	queue   chan op
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
}

type op struct {
	kind    byte // 'e' event, 'a' audit, 'm' metrics, 'f' flush marker
	event   *adminv1.Event
	audit   AuditEntry
	metrics HostMetricsSample
	ack     chan struct{} // closed by the writer once everything before it is committed
}

// queueSize bounds what the writer may fall behind by before appends
// block the receiving path. A burst of events beyond it waits for the
// writer instead of being dropped: the store is retention, and retention
// that drops under load is not.
const queueSize = 4096

// Open creates dir if needed, opens (or creates) FileName inside it, and
// starts the writer. retention <= 0 keeps everything until Purge is
// called with an explicit cutoff.
func Open(dir string, retention time.Duration) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty data directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	path := filepath.Join(dir, FileName)
	// WAL keeps readers off the writer's lock; busy_timeout covers the
	// checkpoint window; synchronous=NORMAL is durable across process
	// death under WAL, which is the failure retention exists for.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection: SQLite has one writer, and the store has one
	// writer goroutine; readers share the same connection through the
	// pool's serialization, which is simpler than reasoning about WAL
	// snapshots per connection.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{
		db:        db,
		retention: retention,
		now:       time.Now,
		queue:     make(chan op, queueSize),
		done:      make(chan struct{}),
	}
	go s.writer()
	return s, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			ts      INTEGER NOT NULL,
			node_id TEXT    NOT NULL,
			kind    INTEGER NOT NULL,
			body    BLOB    NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS events_ts ON events (ts)`,
		`CREATE INDEX IF NOT EXISTS events_kind_id ON events (kind, id)`,
		`CREATE TABLE IF NOT EXISTS audit (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			ts      INTEGER NOT NULL,
			actor   TEXT    NOT NULL,
			action  TEXT    NOT NULL,
			target  TEXT    NOT NULL,
			node_id TEXT    NOT NULL,
			before  TEXT    NOT NULL,
			after   TEXT    NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS audit_ts ON audit (ts)`,
		`CREATE TABLE IF NOT EXISTS host_metrics (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			ts      INTEGER NOT NULL,
			node_id TEXT    NOT NULL,
			body    BLOB    NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS host_metrics_node_ts ON host_metrics (node_id, ts)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// Retention is the window the store was opened with.
func (s *Store) Retention() time.Duration { return s.retention }

// cutoff is the oldest instant a read returns; zero when there is no
// window.
func (s *Store) cutoff() time.Time {
	if s.retention <= 0 {
		return time.Time{}
	}
	return s.now().Add(-s.retention)
}

// AppendEvent queues an event. Its timestamp is the event's own; an event
// without one is stamped now.
func (s *Store) AppendEvent(e *adminv1.Event) {
	if s == nil || e == nil {
		return
	}
	s.enqueue(op{kind: 'e', event: e})
}

// AppendAudit queues an audit entry.
func (s *Store) AppendAudit(e AuditEntry) {
	if s == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = s.now()
	}
	s.enqueue(op{kind: 'a', audit: e})
}

// AppendHostMetrics queues one sample.
func (s *Store) AppendHostMetrics(nodeID string, m *adminv1.HostMetrics) {
	if s == nil || m == nil {
		return
	}
	s.enqueue(op{kind: 'm', metrics: HostMetricsSample{Time: s.now(), NodeID: nodeID, Metrics: m}})
}

func (s *Store) enqueue(o op) {
	s.closeMu.Lock()
	closed := s.closed
	s.closeMu.Unlock()
	if closed {
		return
	}
	s.queue <- o
}

// writer drains the queue in batches: one transaction per batch.
func (s *Store) writer() {
	defer close(s.done)
	for o, ok := <-s.queue; ok; o, ok = <-s.queue {
		batch := []op{o}
	drain:
		for len(batch) < 256 {
			select {
			case next, more := <-s.queue:
				if !more {
					break drain
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}
		s.writeBatch(batch)
		for _, o := range batch {
			if o.kind == 'f' {
				close(o.ack)
			}
		}
	}
}

func (s *Store) writeBatch(batch []op) {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	for _, o := range batch {
		switch o.kind {
		case 'e':
			body, err := proto.Marshal(o.event)
			if err != nil {
				continue
			}
			ts := o.event.GetTimestamp().AsTime()
			if o.event.GetTimestamp() == nil {
				ts = s.now()
			}
			_, _ = tx.Exec(`INSERT INTO events (ts, node_id, kind, body) VALUES (?, ?, ?, ?)`,
				ts.UnixNano(), o.event.GetNodeId(), int(kindOf(o.event)), body)
		case 'a':
			_, _ = tx.Exec(`INSERT INTO audit (ts, actor, action, target, node_id, before, after) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				o.audit.Time.UnixNano(), o.audit.Actor, o.audit.Action, o.audit.Target, o.audit.NodeID, o.audit.Before, o.audit.After)
		case 'm':
			body, err := proto.Marshal(o.metrics.Metrics)
			if err != nil {
				continue
			}
			_, _ = tx.Exec(`INSERT INTO host_metrics (ts, node_id, body) VALUES (?, ?, ?)`,
				o.metrics.Time.UnixNano(), o.metrics.NodeID, body)
		}
	}
	_ = tx.Commit()
}

// Flush blocks until everything queued before the call is written. It is
// what a reader calls before reading its own writes.
func (s *Store) Flush(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	marker := op{kind: 'f', ack: make(chan struct{})}
	s.queue <- marker
	s.closeMu.Unlock()
	select {
	case <-marker.ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RecentEvents returns up to limit events of the kind, newest last,
// within the window. limit <= 0 returns everything within the window.
func (s *Store) RecentEvents(kind adminv1.EventType, limit int) ([]*adminv1.Event, error) {
	if s == nil {
		return nil, nil
	}
	q := `SELECT body FROM events WHERE kind = ? AND ts >= ? ORDER BY id DESC`
	args := []any{int(kind), s.cutoff().UnixNano()}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: recent events: %w", err)
	}
	defer rows.Close()
	var out []*adminv1.Event
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		ev := &adminv1.Event{}
		if err := proto.Unmarshal(body, ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	// Newest last, the order the replay ring hands them out.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// ListAudit returns up to limit entries, newest first, within the window.
func (s *Store) ListAudit(limit int) ([]AuditEntry, error) {
	if s == nil {
		return nil, nil
	}
	q := `SELECT ts, actor, action, target, node_id, before, after FROM audit WHERE ts >= ? ORDER BY id DESC`
	args := []any{s.cutoff().UnixNano()}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&ts, &e.Actor, &e.Action, &e.Target, &e.NodeID, &e.Before, &e.After); err != nil {
			return nil, err
		}
		e.Time = time.Unix(0, ts).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// HostMetricsHistory returns the node's samples within the window and
// not older than since (zero: the whole window), oldest first, at most
// limit (<= 0: all).
func (s *Store) HostMetricsHistory(nodeID string, since time.Time, limit int) ([]HostMetricsSample, error) {
	if s == nil {
		return nil, nil
	}
	from := s.cutoff()
	if since.After(from) {
		from = since
	}
	q := `SELECT ts, body FROM host_metrics WHERE node_id = ? AND ts >= ? ORDER BY id DESC`
	args := []any{nodeID, from.UnixNano()}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: host metrics history: %w", err)
	}
	defer rows.Close()
	var out []HostMetricsSample
	for rows.Next() {
		var ts int64
		var body []byte
		if err := rows.Scan(&ts, &body); err != nil {
			return nil, err
		}
		m := &adminv1.HostMetrics{}
		if err := proto.Unmarshal(body, m); err != nil {
			continue
		}
		out = append(out, HostMetricsSample{Time: time.Unix(0, ts).UTC(), NodeID: nodeID, Metrics: m})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// Purge deletes every row older than the window and returns how many
// went. With no window it deletes nothing.
func (s *Store) Purge() (int64, error) {
	if s == nil || s.retention <= 0 {
		return 0, nil
	}
	cut := s.cutoff().UnixNano()
	var total int64
	for _, table := range []string{"events", "audit", "host_metrics"} {
		res, err := s.db.Exec(`DELETE FROM `+table+` WHERE ts < ?`, cut)
		if err != nil {
			return total, fmt.Errorf("store: purge %s: %w", table, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// Close flushes the queue and closes the file. Appends after Close are
// dropped.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.closeMu.Unlock()
	<-s.done
	return s.db.Close()
}

func kindOf(e *adminv1.Event) adminv1.EventType {
	switch e.GetBody().(type) {
	case *adminv1.Event_HttpRequest:
		return adminv1.EventType_EVENT_TYPE_HTTP_REQUEST
	case *adminv1.Event_SqlStatement:
		return adminv1.EventType_EVENT_TYPE_SQL_STATEMENT
	case *adminv1.Event_SessionChange:
		return adminv1.EventType_EVENT_TYPE_SESSION_CHANGE
	case *adminv1.Event_Custom:
		return adminv1.EventType_EVENT_TYPE_CUSTOM
	}
	return adminv1.EventType_EVENT_TYPE_UNSPECIFIED
}
