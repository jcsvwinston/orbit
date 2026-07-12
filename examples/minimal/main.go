// Command minimal is the smallest runnable Orbit example: a Nucleus app
// with the in-process admin panel (orbit.Module) mounted at /admin.
//
// Run it:
//
//	ADMIN_BOOTSTRAP_PASSWORD=change-me-please go run .
//
// then open http://localhost:8080/admin and sign in as "admin" with that
// password. With ADMIN_BOOTSTRAP_PASSWORD unset, bootstrapping is skipped
// (no admin user is created) — the framework's secure default; provision
// the admin another way (e.g. `nucleus createuser`) or set the variable.
package main

import (
	"log"
	"os"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/orbit"
)

func main() {
	app, err := nucleus.New().
		FromConfigFile("nucleus.yaml").
		Mount(orbit.Module(orbit.Config{
			Prefix:            "/admin",
			Title:             "Minimal Admin",
			BootstrapUsername: "admin",
			BootstrapEmail:    "admin@example.test",
			// Empty password → bootstrapping is skipped (safe default).
			BootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
		})).
		Build()
	if err != nil {
		log.Fatalf("build app: %v", err)
	}

	if os.Getenv("ADMIN_BOOTSTRAP_PASSWORD") == "" {
		log.Println("note: ADMIN_BOOTSTRAP_PASSWORD is unset — no admin user will be " +
			"created. Set it (>= 8 chars) to seed the first admin, then open /admin.")
	}
	log.Println("orbit minimal example listening on http://localhost:8080 (admin at /admin)")

	if err := nucleus.Run(app); err != nil {
		log.Fatalf("run: %v", err)
	}
}
