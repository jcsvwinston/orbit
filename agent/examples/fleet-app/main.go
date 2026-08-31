// Command fleet-app is a minimal Nucleus host process wired with the Orbit
// cluster agent (orbit/agent). The agent ships this process's HTTP/SQL
// observability over a bidi stream to a standalone admin server
// (orbit/server) so an operator can watch the fleet from one place.
//
// End-to-end, two processes (this app serves on :8000 so the admin
// server's UI can keep :8080):
//
//  1. Start the admin server (from the orbit/server module):
//
//     go run ./cmd/admin-server --agent-addr=127.0.0.1:9090 --ui-addr=127.0.0.1:8080 --ui-insecure-open
//
//     --ui-insecure-open makes the UI loadable from a plain browser in
//     local dev (loopback only); without it the UI listener expects a
//     header-setting reverse proxy or a bearer and answers 401.
//
//  2. Start this app pointing at it:
//
//     ORBIT_ADMIN_ENDPOINT=http://127.0.0.1:9090 go run .
//
// Open http://127.0.0.1:8080 and this node appears in the topology;
// traffic to this app (http://127.0.0.1:8000) streams into the live feed.
//
// With ORBIT_ADMIN_ENDPOINT unset the agent stays disabled and the app runs
// unchanged — the agent is strictly opt-in and fail-open, never on the
// framework's hot path.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	orbitagent "github.com/jcsvwinston/orbit/agent"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func main() {
	cfg, err := app.LoadConfig("nucleus.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	var opts []app.Option
	if ep := os.Getenv("ORBIT_ADMIN_ENDPOINT"); ep != "" {
		opts = append(opts, app.WithExtensions(
			orbitagent.NewExtension(orbitagent.ExtensionConfig{
				Endpoints: []string{ep},
				// Must match the admin server's --agent-token when it sets one.
				Token: os.Getenv("ORBIT_ADMIN_TOKEN"),
			}, ".orbit-agent-state", "example"),
		))
		log.Printf("orbit agent enabled → %s", ep)
	} else {
		log.Println("ORBIT_ADMIN_ENDPOINT unset → agent disabled (app runs unchanged)")
	}

	a, err := app.New(cfg, opts...)
	if err != nil {
		log.Fatalf("new app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("fleet-app listening (Ctrl-C to stop)")
	if err := a.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}
}
