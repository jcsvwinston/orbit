package admin

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// adminSQLURL selects the engine the bootstrap-admin live tests run against,
// mirroring the ORBIT_TEST_*_URL convention of server/datastudio_integration_test.go.
//
// The default keeps the fast lane dependency-free (in-memory SQLite), but
// SQLite is the engine LEAST able to expose the defect these tests guard:
// its driver reports constraint failures in English, always. Point this at a
// real server — ideally one whose messages are NOT in English — to run the
// identical assertions where the bug actually bites:
//
//	ORBIT_TEST_ADMIN_SQL_URL='postgres://postgres:secret@localhost:55432/orbit?sslmode=disable' go test ./internal/admin/
func adminSQLURL() string {
	if u := strings.TrimSpace(os.Getenv("ORBIT_TEST_ADMIN_SQL_URL")); u != "" {
		return u
	}
	return "sqlite://:memory:"
}

// TestBootstrapDuplicate_ClassifiesLiveEngineError pins that a unique
// violation raised by a REAL engine is recognised as "the admin user already
// exists" rather than escalated into a startup failure.
//
// The regression it guards is a localisation bug, which is why the error must
// come from a live server and not from a hand-built fixture: the classifier
// used to match English substrings of the driver's message ("duplicate key",
// "unique constraint", …). PostgreSQL, MySQL, Oracle and SQL Server all
// translate those messages when the server runs in another language — a
// PostgreSQL server with lc_messages='es_ES.utf8' answers
//
//	llave duplicada viola restricción de unicidad «nucleus_admin_users_username_key»
//
// in which none of the English substrings appear. The duplicate then went
// unrecognised and EnsureBootstrapAdminUser returned an error, which
// orbit.go turns into a failure to start the module.
func TestBootstrapDuplicate_ClassifiesLiveEngineError(t *testing.T) {
	ctx := context.Background()
	database, err := db.New(db.Config{DatabaseURL: adminSQLURL()}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	system := database.System()

	if _, err := sqlDB.ExecContext(ctx, "DROP TABLE "+defaultAdminUsersTable); err != nil {
		t.Logf("drop (ignored, table may not exist): %v", err)
	}
	if err := ensureBootstrapAdminUsersTable(ctx, sqlDB, system); err != nil {
		t.Fatalf("ensure table: %v", err)
	}

	row := bootstrapAdminRow{
		id: "u_seed", username: "root", email: "root@example.com",
		passwordHash: "x", isSuperuser: 1, createdAt: "now", updatedAt: "now",
	}
	if err := insertBootstrapAdminUser(ctx, sqlDB, system, row); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	// Same username, everything else distinct: the only constraint that can
	// reject this row is the unique index on username.
	dup := row
	dup.id = "u_dup"
	dup.email = "other@example.com"
	dupErr := insertBootstrapAdminUser(ctx, sqlDB, system, dup)
	if dupErr == nil {
		t.Fatal("inserting a second row with the same username must violate the unique constraint")
	}
	t.Logf("engine (%s) reported: %v", system, dupErr)

	if !isBootstrapDuplicateError(dupErr) {
		t.Errorf("isBootstrapDuplicateError did not recognise a real engine violation: %v", dupErr)
	}
}

// TestEnsureBootstrapAdminUser_ConcurrentFirstBoot exercises the only
// situation in which the duplicate branch is reachable at all, and it is
// worth being precise about which one that is.
//
// A plain restart never reaches it: countBootstrapAdminUsers finds the
// existing admin and returns before the INSERT. The branch fires when the
// COUNT and the INSERT straddle another writer's commit — several replicas
// booting at once against an empty admin table, which is the normal shape of
// a first deploy at more than one replica. One wins the insert; the others
// must quietly conclude "the user already exists" and carry on. Before the
// fix, on a server not running in English, they instead returned an error
// that orbit's OnStart propagates, so nucleus aborts the whole application:
//
//	nucleus: module "orbit" OnStart: orbit: ensure bootstrap admin user: …
//
// The losing replicas crash-loop while the winner serves.
//
// The race is staged deterministically rather than by racing goroutines: an
// open transaction holds the winning row uncommitted, so the call under test
// sees an empty table on its COUNT and then blocks on the unique index until
// that transaction commits. That is the real engine behaviour the production
// race relies on, with the timing pinned.
func TestEnsureBootstrapAdminUser_ConcurrentFirstBoot(t *testing.T) {
	ctx := context.Background()
	database, err := db.New(db.Config{DatabaseURL: adminSQLURL()}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	system := database.System()

	if system == "sqlite" {
		// SQLite is single-writer: the second connection would be refused
		// with SQLITE_BUSY rather than a unique violation, which is a
		// different (and correct) failure. Nothing to assert here.
		t.Skip("sqlite is single-writer; set ORBIT_TEST_ADMIN_SQL_URL to a concurrent engine")
	}

	if _, err := sqlDB.ExecContext(ctx, "DROP TABLE "+defaultAdminUsersTable); err != nil {
		t.Logf("drop (ignored, table may not exist): %v", err)
	}
	if err := ensureBootstrapAdminUsersTable(ctx, sqlDB, system); err != nil {
		t.Fatalf("ensure table: %v", err)
	}

	// The replica that wins the race, holding its row uncommitted.
	winner, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin winner tx: %v", err)
	}
	defer func() { _ = winner.Rollback() }()
	ph := bootstrapInsertPlaceholders(system)
	if ph == nil {
		t.Fatalf("no bind placeholders for system %q", system)
	}
	winnerStmt := "INSERT INTO " + defaultAdminUsersTable + " " + adminUsersInsertColumns +
		" VALUES (" + strings.Join(ph, ", ") + ")"
	if _, err := winner.ExecContext(ctx, winnerStmt,
		"u_winner", "root", "root@example.com", "x", 1, "now", "now"); err != nil {
		t.Fatalf("winner insert: %v", err)
	}

	// The replica that loses: it counts an empty table, then blocks on the
	// unique index until the winner commits.
	type outcome struct {
		res BootstrapAdminResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := EnsureBootstrapAdminUser(ctx, sqlDB, BootstrapAdminConfig{
			Username: "root",
			Email:    "root@example.com",
			Password: "supersecret",
			System:   system,
		})
		done <- outcome{res, err}
	}()

	// Give the loser time to get past its COUNT and onto the blocking INSERT,
	// then let the winner through.
	time.Sleep(300 * time.Millisecond)
	if err := winner.Commit(); err != nil {
		t.Fatalf("commit winner: %v", err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("the losing replica must not fail to start; got: %v", got.err)
		}
		if got.res.Created {
			t.Error("the losing replica must not report having created the admin user")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("EnsureBootstrapAdminUser did not return after the winner committed")
	}

	var count int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+defaultAdminUsersTable).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("admin user count: got %d want 1 (exactly one replica may win)", count)
	}
}
