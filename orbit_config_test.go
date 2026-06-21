package orbit

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// Module accepts a Config populated with the cluster/live/trace/auth-database
// options and still returns a well-formed orbit spec.
func TestModule_WithParityConfig(t *testing.T) {
	spec := Module(Config{
		Prefix:              "/ops",
		AuthDatabase:        "auth",
		LiveExcludePatterns: []string{"/healthz", "/metrics"},
		ClusterEnabled:      true,
		ClusterRedisURL:     "redis://localhost:6379/0",
		ClusterChannel:      "orbit:live",
		ClusterNodeID:       "node-a",
		ClusterToken:        "shared-secret",
		TraceURLTemplate:    "https://trace.example/{trace_id}",
	})
	if spec.Name() != "orbit" {
		t.Errorf("Name = %q, want orbit", spec.Name())
	}
	if spec.Prefix() != "/ops" {
		t.Errorf("Prefix = %q, want /ops", spec.Prefix())
	}
}

// resolveAuthDB with an empty alias returns the default handle's *sql.DB and
// the default handle's dialect.
func TestResolveAuthDB_DefaultsWhenEmpty(t *testing.T) {
	database, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	defaultSQL, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	got, system, err := resolveAuthDB("", database, map[string]*db.DB{"default": database})
	if err != nil {
		t.Fatalf("resolveAuthDB: %v", err)
	}
	if got != defaultSQL {
		t.Errorf("resolveAuthDB(\"\") db = %p, want default %p", got, defaultSQL)
	}
	if system != database.System() {
		t.Errorf("resolveAuthDB(\"\") system = %q, want %q", system, database.System())
	}
}

// resolveAuthDB with a known alias returns THAT handle's *sql.DB and dialect,
// not the default's.
func TestResolveAuthDB_ResolvesNamedAlias(t *testing.T) {
	def, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New (default): %v", err)
	}
	defer def.Close()
	defaultSQL, err := def.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB (default): %v", err)
	}

	authDB, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New (auth): %v", err)
	}
	defer authDB.Close()
	authSQL, err := authDB.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB (auth): %v", err)
	}

	handles := map[string]*db.DB{"default": def, "auth": authDB}
	got, system, err := resolveAuthDB("auth", def, handles)
	if err != nil {
		t.Fatalf("resolveAuthDB: %v", err)
	}
	if got != authSQL {
		t.Errorf("resolveAuthDB(\"auth\") db = %p, want auth %p", got, authSQL)
	}
	if got == defaultSQL {
		t.Error("resolveAuthDB(\"auth\") returned the default handle, want the auth handle")
	}
	if system != authDB.System() {
		t.Errorf("resolveAuthDB(\"auth\") system = %q, want %q", system, authDB.System())
	}
}

// resolveAuthDB with an unknown alias returns a clear, wrapped error.
func TestResolveAuthDB_UnknownAliasErrors(t *testing.T) {
	def, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer def.Close()

	_, _, err = resolveAuthDB("missing", def, map[string]*db.DB{"default": def})
	if err == nil {
		t.Fatal("resolveAuthDB(\"missing\") = nil error, want a not-found error")
	}
}
