package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	_ "modernc.org/sqlite"
)

func TestAdminUserLookupSQL_Dialects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		system  string
		byID    string
		byLogin string
	}{
		{"sqlite", "WHERE id = ?", "WHERE LOWER(username) = LOWER(?) OR LOWER(email) = LOWER(?)"},
		{"mysql", "WHERE id = ?", "WHERE LOWER(username) = LOWER(?) OR LOWER(email) = LOWER(?)"},
		{"postgresql", "WHERE id = $1", "WHERE LOWER(username) = LOWER($1) OR LOWER(email) = LOWER($2)"},
		{"mssql", "WHERE id = @p1", "WHERE LOWER(username) = LOWER(@p1) OR LOWER(email) = LOWER(@p2)"},
		{"oracle", "WHERE id = :1", "WHERE LOWER(username) = LOWER(:1) OR LOWER(email) = LOWER(:2)"},
	}
	for _, tc := range cases {
		q, ok := adminUserByIDSQL("t", tc.system)
		if !ok || !strings.HasSuffix(q, tc.byID) || !strings.HasPrefix(q, "SELECT "+adminUserSelectColumns+" FROM t ") {
			t.Errorf("%s byID = %q (ok=%v), want suffix %q", tc.system, q, ok, tc.byID)
		}
		q, ok = adminUserByLoginSQL("t", tc.system)
		if !ok || !strings.HasSuffix(q, tc.byLogin) {
			t.Errorf("%s byLogin = %q (ok=%v), want suffix %q", tc.system, q, ok, tc.byLogin)
		}
	}
	for _, system := range []string{"", "unknown"} {
		if _, ok := adminUserByIDSQL("t", system); ok {
			t.Errorf("byID(%q) ok, want the full-read fallback", system)
		}
		if _, ok := adminUserByLoginSQL("t", system); ok {
			t.Errorf("byLogin(%q) ok, want the full-read fallback", system)
		}
	}
	if got := bindPlaceholders("postgresql", 0); got != nil {
		t.Errorf("bindPlaceholders(n=0) = %v, want nil", got)
	}
}

// openAdminUsersSQLite returns a one-connection in-memory SQLite with the
// admin table (nullable columns, like a table provisioned by hand).
func openAdminUsersSQLite(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return sqlDB
}

func seedAdminUsers(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	rows := [][]any{
		{"1", "Alice", "alice@example.com", "h1", 1},
		{"2", "bob", "Bob@Example.com", "h2", 0},
		{"3", "carol", "carol@example.com", "h3", "true"},
	}
	for _, r := range rows {
		if _, err := sqlDB.Exec(`INSERT INTO nucleus_admin_users VALUES (?,?,?,?,?)`, r...); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
}

// TestFindUser_BoundedQueryReadsOnlyTheRequestedRow is the behavioural
// proof that the lookup no longer reads the whole table: a second row that
// cannot be scanned (NULL username) used to break EVERY lookup, because the
// full read scanned it on the way to the row asked for. With WHERE it is
// never read.
func TestFindUser_BoundedQueryReadsOnlyTheRequestedRow(t *testing.T) {
	sqlDB := openAdminUsersSQLite(t)
	seedAdminUsers(t, sqlDB)
	if _, err := sqlDB.Exec(`INSERT INTO nucleus_admin_users (id, username, email, password_hash, is_superuser) VALUES ('9', NULL, NULL, 'h9', 0)`); err != nil {
		t.Fatal(err)
	}

	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin").WithSystem("sqlite")
	ctx := context.Background()

	rec, found, err := a.findUserByID(ctx, "1")
	if err != nil || !found || rec.Username != "Alice" || !rec.IsSuperuser {
		t.Fatalf("findUserByID(1) = %+v found=%v err=%v, want Alice (superuser)", rec, found, err)
	}
	rec, found, err = a.findUserByLogin(ctx, "bob@example.com")
	if err != nil || !found || rec.ID != "2" {
		t.Fatalf("findUserByLogin(bob@example.com) = %+v found=%v err=%v, want id 2", rec, found, err)
	}
}

func TestFindUser_BoundedAndFallbackAgree(t *testing.T) {
	sqlDB := openAdminUsersSQLite(t)
	seedAdminUsers(t, sqlDB)
	ctx := context.Background()

	for _, system := range []string{"sqlite", ""} {
		a := NewDatabaseAdminAuth(sqlDB, nil, "/admin").WithSystem(system)
		t.Run("system="+system, func(t *testing.T) {
			// By id: hit, miss.
			if rec, found, err := a.findUserByID(ctx, " 3 "); err != nil || !found || rec.Username != "carol" || !rec.IsSuperuser {
				t.Errorf("byID(3) = %+v found=%v err=%v", rec, found, err)
			}
			if _, found, err := a.findUserByID(ctx, "42"); err != nil || found {
				t.Errorf("byID(42) found=%v err=%v, want a miss without error", found, err)
			}
			// By login: username and email, case-insensitive.
			for _, login := range []string{"alice", "ALICE", "Alice@Example.com"} {
				if rec, found, err := a.findUserByLogin(ctx, login); err != nil || !found || rec.ID != "1" {
					t.Errorf("byLogin(%q) = %+v found=%v err=%v, want id 1", login, rec, found, err)
				}
			}
			if rec, found, err := a.findUserByLogin(ctx, "bob@example.com"); err != nil || !found || rec.ID != "2" || rec.IsSuperuser {
				t.Errorf("byLogin(bob@example.com) = %+v found=%v err=%v, want id 2 (not superuser)", rec, found, err)
			}
			if _, found, err := a.findUserByLogin(ctx, "nobody"); err != nil || found {
				t.Errorf("byLogin(nobody) found=%v err=%v, want a miss without error", found, err)
			}
		})
	}
}

func TestFindUser_MissingTableIsNotAnError(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin").WithSystem("sqlite")
	if _, found, err := a.findUserByID(context.Background(), "1"); err != nil || found {
		t.Fatalf("missing table: found=%v err=%v, want (false, nil) so the session is destroyed", found, err)
	}
	if _, found, err := a.findUserByLogin(context.Background(), "x"); err != nil || found {
		t.Fatalf("missing table: found=%v err=%v, want (false, nil)", found, err)
	}
}

// TestAuthenticate_DeletedUserDestroysSession protects the revocation
// semantics the bounded query must keep: an admin removed from the table
// loses the session on the next request.
func TestAuthenticate_DeletedUserDestroysSession(t *testing.T) {
	sqlDB := openAdminUsersSQLite(t)
	seedAdminUsers(t, sqlDB)
	sm := auth.NewSessionManager(auth.SessionConfig{})
	a := NewDatabaseAdminAuth(sqlDB, sm, "/admin").WithSystem("sqlite")

	mux := http.NewServeMux()
	mux.HandleFunc("/seed", func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), adminSessionUserIDKey, "2")
	})
	var lastErr error
	var lastUser *auth.User
	mux.HandleFunc("/who", func(w http.ResponseWriter, r *http.Request) {
		lastUser, lastErr = a.Authenticate(r)
	})
	srv := httptest.NewServer(sm.Middleware()(mux))
	defer srv.Close()
	client := newCookieClient(t)

	get := func(path string) {
		t.Helper()
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	get("/seed")
	get("/who")
	if lastErr != nil || lastUser == nil || lastUser.Username != "bob" {
		t.Fatalf("authenticated user = %+v err=%v, want bob", lastUser, lastErr)
	}

	if _, err := sqlDB.Exec(`DELETE FROM nucleus_admin_users WHERE id = '2'`); err != nil {
		t.Fatal(err)
	}
	get("/who")
	if lastErr == nil || lastUser != nil {
		t.Fatalf("deleted user still authenticates: %+v", lastUser)
	}
	// The session was destroyed: even if the row came back, the cookie no
	// longer names a user.
	if _, err := sqlDB.Exec(`INSERT INTO nucleus_admin_users VALUES ('2','bob','bob@example.com','h2',0)`); err != nil {
		t.Fatal(err)
	}
	get("/who")
	if lastErr == nil || lastUser != nil {
		t.Fatalf("session survived the revocation: %+v", lastUser)
	}
}

// TestFindUser_BoundedQueryOnLiveEngine runs the bounded lookups against
// ORBIT_TEST_ADMIN_SQL_URL when set (Postgres, MySQL), so the placeholder
// style of each dialect is exercised by a real server, not only by the SQL
// text assertions above. On the default SQLite URL it is the same table the
// bootstrap creates.
func TestFindUser_BoundedQueryOnLiveEngine(t *testing.T) {
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
	if system != "sqlite" {
		if _, err := sqlDB.ExecContext(ctx, "DROP TABLE "+defaultAdminUsersTable); err != nil {
			t.Logf("drop (ignored, table may not exist): %v", err)
		}
	}
	if err := ensureBootstrapAdminUsersTable(ctx, sqlDB, system); err != nil {
		t.Fatalf("ensure table: %v", err)
	}
	if err := insertBootstrapAdminUser(ctx, sqlDB, system, bootstrapAdminRow{
		id: "live-1", username: "Live", email: "live@example.com", passwordHash: "h", isSuperuser: 1,
		createdAt: "2026-09-05T00:00:00Z", updatedAt: "2026-09-05T00:00:00Z",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin").WithSystem(system)
	if rec, found, err := a.findUserByID(ctx, "live-1"); err != nil || !found || rec.Username != "Live" || !rec.IsSuperuser {
		t.Fatalf("[%s] byID = %+v found=%v err=%v", system, rec, found, err)
	}
	if rec, found, err := a.findUserByLogin(ctx, "LIVE@example.com"); err != nil || !found || rec.ID != "live-1" {
		t.Fatalf("[%s] byLogin = %+v found=%v err=%v", system, rec, found, err)
	}
	if _, found, err := a.findUserByID(ctx, "nope"); err != nil || found {
		t.Fatalf("[%s] byID(nope) found=%v err=%v", system, found, err)
	}
}
