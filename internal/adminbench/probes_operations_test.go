// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// probeSessionList asks the panel who is signed in. The panel has served this
// view since before the arc; what changed under it is that the framework now
// records the device a session belongs to (nucleus v1.28.0), which is the
// half this probe checks for.
func probeSessionList(t *testing.T, e *env) verdict {
	e.operatorNamed(t, "ops-sessions") // a second signed-in session, so the list has more than one row

	r := e.get(t, "/admin/api/sessions")
	if r.code != http.StatusOK {
		t.Logf("GET /admin/api/sessions answered %d: %s", r.code, r.text())
		return absent
	}
	payload := r.json(t)
	if enabled, _ := payload["enabled"].(bool); !enabled {
		t.Logf("the panel serves the view disabled: %v", payload["reason"])
		return absent
	}
	rows, _ := payload["sessions"].([]any)
	if len(rows) == 0 {
		t.Logf("no sessions listed while two are signed in: %s", r.text())
		return absent
	}
	return present
}

// probeSessionDevice is the criterion A5 moved here: the operator looking at
// the list has to be able to tell "that one is my phone" from "that one is
// not me".
func probeSessionDevice(t *testing.T, e *env) verdict {
	e.operatorNamed(t, "ops-sessions")
	r := e.get(t, "/admin/api/sessions")
	if r.code != http.StatusOK {
		return absent
	}
	rows, _ := r.json(t)["sessions"].([]any)
	device, ip := 0, 0
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"user_agent", "device", "client", "browser", "platform"} {
			if text, _ := entry[key].(string); strings.TrimSpace(text) != "" {
				device++
				break
			}
		}
		if text, _ := entry["remote_ip"].(string); strings.TrimSpace(text) != "" {
			ip++
		}
	}
	t.Logf("%d session rows: %d name a device, %d carry an address", len(rows), device, ip)
	switch {
	case device > 0:
		return present
	case ip > 0:
		return partial
	default:
		return absent
	}
}

// probeSessionRevoke signs an operator in, revokes that session from the
// panel, and checks the operator is actually locked out.
func probeSessionRevoke(t *testing.T, e *env) verdict {
	victim := e.newOperator(t, "revocable", false)
	before := e.asOperator(t, victim, http.MethodGet, "/admin/api/health", nil)
	if before.code == http.StatusUnauthorized {
		t.Logf("the operator was not signed in to begin with")
		return absent
	}

	// The row id is a one-way handle over the session token (the panel never
	// serializes the token itself), so the probe derives the handle the same
	// way and revokes exactly that session.
	handle := sessionHandleOf(t, e, victim)
	if handle == "" {
		t.Logf("the operator holds no session cookie to address")
		return absent
	}
	list := e.get(t, "/admin/api/sessions")
	if !strings.Contains(list.raw(), handle) {
		t.Logf("the handle the panel would use is not in the list it serves: %s", list.text())
		return partial
	}
	kill := e.do(t, http.MethodDelete, "/admin/api/sessions/"+handle, nil)
	if kill.code >= 400 {
		t.Logf("terminate answered %d: %s", kill.code, kill.text())
		return partial
	}
	after := e.asOperator(t, victim, http.MethodGet, "/admin/api/models", nil)
	if after.code == http.StatusOK {
		t.Logf("the revoked session still works (%d)", after.code)
		return partial
	}
	return present
}

// probeSessionRevokeAll asks for the button an incident needs: every session
// of one account, gone. The framework grew RevokeWhere in the previous arc;
// this measures whether the panel spends it.
func probeSessionRevokeAll(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "ops-revoke-all")
	return e.unrouted(t,
		"/admin/api/sessions/revoke-all",
		"/admin/api/users/"+op.id+"/sessions",
		"/admin/api/sessions/user/"+op.username)
}

// probeLiveFeed reads the live request feed — the capability no competitor in
// the comparison has.
func probeLiveFeed(t *testing.T, e *env) verdict {
	// Traffic the panel will actually record: it excludes its own prefix by
	// default, so asking the admin API and then looking for it in the feed
	// measures the exclusion, not the feed.
	resp, err := e.operator(t).Get(e.server().URL("/healthz"))
	if err != nil {
		t.Fatalf("application traffic: %v", err)
	}
	_ = resp.Body.Close()

	r := e.get(t, "/admin/api/live/snapshot")
	if r.code != http.StatusOK {
		t.Logf("live snapshot answered %d: %s", r.code, r.text())
		return absent
	}
	requests, _ := r.json(t)["requests"].([]any)
	if len(requests) == 0 {
		t.Logf("the snapshot shows no traffic at all: %s", r.text())
		return partial
	}
	seen := false
	for _, item := range requests {
		if entry, ok := item.(map[string]any); ok {
			if path, _ := entry["path"].(string); strings.Contains(path, "/healthz") {
				seen = true
			}
		}
	}
	if !seen {
		t.Logf("%d requests recorded, none of them the one just made: %s", len(requests), r.text())
		return partial
	}
	return present
}

// probeLiveStream opens the websocket an operator's live view holds open.
func probeLiveStream(t *testing.T, e *env) verdict {
	srv := e.server()
	wsURL := strings.Replace(srv.URL("/admin/api/live/ws"), "http://", "ws://", 1)
	cfg, err := websocket.NewConfig(wsURL, srv.URL("/"))
	if err != nil {
		t.Fatalf("websocket config: %v", err)
	}
	parsed, err := http.NewRequest(http.MethodGet, srv.URL("/admin"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for _, cookie := range e.operator(t).Jar.Cookies(parsed.URL) {
		cfg.Header.Add("Cookie", cookie.String())
	}
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Logf("dial %s: %v", wsURL, err)
		return absent
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	var hello map[string]any
	if err := websocket.JSON.Receive(conn, &hello); err != nil {
		t.Logf("the stream opened and said nothing: %v", err)
		return partial
	}
	t.Logf("stream hello: %v", hello)
	return present
}

// probeSystemPulse reads the runtime numbers that sit next to the CRUD — the
// "admin plus light APM" claim.
func probeSystemPulse(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/system/snapshot")
	if r.code != http.StatusOK {
		t.Logf("system snapshot answered %d: %s", r.code, r.text())
		return absent
	}
	body := r.raw()
	t.Logf("pulse keys: %s", strings.Join(topLevelKeys(r.json(t)), " "))
	missing := []string{}
	for _, key := range []string{"goroutine", "memory", "database"} {
		if !strings.Contains(strings.ToLower(body), key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Logf("the snapshot has no %v: %s", missing, body)
		return partial
	}
	return present
}

// probeHealth is the panel's own health view.
func probeHealth(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/health")
	if r.code != http.StatusOK {
		t.Logf("health answered %d: %s", r.code, r.text())
		return absent
	}
	return present
}

// probeMigrations lists what the schema is waiting for and applies it.
func probeMigrations(t *testing.T, e *env) verdict {
	// The default application has no migrations directory, and the panel
	// answers 500 to that (measured separately; it is a defect, not a missing
	// capability). What this control asks is whether an application that DOES
	// have migrations can list and apply them from the panel.
	srv, client := e.migrationsApp(t)

	list := requestAs(t, client, srv, http.MethodGet, "/admin/api/migrations", nil)
	if list.code != http.StatusOK {
		t.Logf("migrations answered %d: %s", list.code, list.text())
		return absent
	}
	if !strings.Contains(list.raw(), "adminbench") {
		t.Logf("the view does not list the migration the application ships: %s", list.text())
		return partial
	}
	apply := requestAs(t, client, srv, http.MethodPost, "/admin/api/migrations/apply", map[string]any{})
	if apply.code >= 400 {
		t.Logf("apply answered %d: %s", apply.code, apply.text())
		return partial
	}
	return present
}

// probeMigrationsMissingDirectory measures what the default application gets:
// a view that fails instead of one that says there is nothing to apply.
func probeMigrationsMissingDirectory(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/migrations")
	t.Logf("an application with no migrations directory gets %d: %s", r.code, r.text())
	if r.code == http.StatusOK {
		return present
	}
	return absent
}

// probeFeatureFlags turns a flag on from the panel and reads it back.
func probeFeatureFlags(t *testing.T, e *env) verdict {
	create := e.do(t, http.MethodPost, "/admin/api/system/flags",
		map[string]any{"name": "adminbench_toggle", "enabled": false})
	if create.code >= 400 {
		t.Logf("create flag answered %d: %s", create.code, create.text())
		return absent
	}
	set := e.do(t, http.MethodPut, "/admin/api/system/flags/adminbench_toggle",
		map[string]any{"enabled": true})
	if set.code >= 400 {
		t.Logf("set flag answered %d: %s", set.code, set.text())
		return partial
	}
	list := e.get(t, "/admin/api/system/flags")
	if !strings.Contains(list.raw(), "adminbench_toggle") {
		t.Logf("the flag is not in the list: %s", list.text())
		return partial
	}
	return present
}

// probeCache reads the cache stats and flushes.
func probeCache(t *testing.T, e *env) verdict {
	stats := e.get(t, "/admin/api/cache")
	if stats.code != http.StatusOK {
		t.Logf("cache stats answered %d: %s", stats.code, stats.text())
		return absent
	}
	flush := e.do(t, http.MethodPost, "/admin/api/cache/flush", map[string]any{})
	if flush.code >= 400 {
		t.Logf("flush answered %d: %s", flush.code, flush.text())
		return partial
	}
	return present
}

// probeStorageBrowse lists what the application stores.
func probeStorageBrowse(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/storage")
	if r.code != http.StatusOK {
		t.Logf("storage answered %d: %s", r.code, r.text())
		return absent
	}
	return present
}

// probeEmailOutbox reads the mail side of the runtime — the arc before this
// one gave the framework product email, so the panel has something to show.
func probeEmailOutbox(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/email")
	if r.code != http.StatusOK {
		t.Logf("email answered %d: %s", r.code, r.text())
		return absent
	}
	body := strings.ToLower(r.text())
	if !strings.Contains(body, "queue") && !strings.Contains(body, "outbox") && !strings.Contains(body, "sent") {
		t.Logf("the email view carries no delivery state: %s", r.text())
		return partial
	}
	return present
}

// probeJobQueues lists the queues and acts on one — pause, resume, retry.
func probeJobQueues(t *testing.T, e *env) verdict {
	list := e.get(t, "/admin/api/jobs")
	if list.code != http.StatusOK {
		t.Logf("jobs answered %d: %s", list.code, list.text())
		return absent
	}
	act := e.do(t, http.MethodPost, "/admin/api/system/jobs/queues/default/actions/pause", map[string]any{})
	if act.code >= 500 {
		t.Logf("pause answered %d: %s", act.code, act.text())
		return partial
	}
	var payload map[string]any
	if err := json.Unmarshal(list.body, &payload); err == nil {
		if queues, ok := payload["queues"].([]any); ok && len(queues) == 0 {
			t.Logf("the queue list is empty in an application with a job runtime: %s", list.text())
			return partial
		}
	}
	return present
}

// probeAsyncExport asks for the big export the browser cannot hold: a job,
// a status, a file.
func probeAsyncExport(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "async-export", "status": "async"})
	create := e.do(t, http.MethodPost, "/admin/api/exports",
		map[string]any{"model": "Note", "format": "csv"})
	if create.code >= 400 {
		t.Logf("create export answered %d: %s", create.code, create.text())
		return absent
	}
	id, _ := create.json(t)["id"].(string)
	if id == "" {
		id, _ = create.json(t)["storage_key"].(string)
	}
	if id == "" {
		t.Logf("the export has no id to poll: %s", create.text())
		return partial
	}
	status := e.get(t, "/admin/api/exports/"+id)
	if status.code != http.StatusOK {
		t.Logf("status answered %d: %s", status.code, status.text())
		return partial
	}
	download := e.get(t, "/admin/api/exports/download?key="+id)
	if download.code != http.StatusOK {
		t.Logf("download answered %d: %s", download.code, download.text())
		return partial
	}
	return present
}

// probeSessionOwner asks the question an operator asks before revoking:
// whose session is this. The row has a field for it.
func probeSessionOwner(t *testing.T, e *env) verdict {
	e.operatorNamed(t, "ops-owner")
	r := e.get(t, "/admin/api/sessions")
	if r.code != http.StatusOK {
		return absent
	}
	rows, _ := r.json(t)["sessions"].([]any)
	named := 0
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if user, _ := entry["user"].(string); strings.TrimSpace(user) != "" {
			named++
		}
	}
	t.Logf("%d of %d session rows name their operator", named, len(rows))
	switch {
	case len(rows) > 0 && named == len(rows):
		return present
	case named > 0:
		return partial
	default:
		return absent
	}
}
