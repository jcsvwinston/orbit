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
//
// It reads ITS OWN operator's row, after that operator has made one request
// through the panel. Two things the first version of this probe got wrong,
// both of the "the probe measures the bench" family: the panel records the
// device of a session on requests that go THROUGH the panel, and a sign-in is
// the framework's login page, so an operator who only signed in has no
// device yet; and the rows the list holds depend on which probes ran before
// this one, so a verdict read over all of them changed with the -run filter.
func probeSessionDevice(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "ops-sessions")
	e.asOperator(t, op, http.MethodGet, "/admin/api/health", nil)
	row := e.sessionRowOf(t, op)
	if row == nil {
		t.Logf("the operator's session is not in the list the panel serves")
		return absent
	}
	device, _ := row["device"].(string)
	agent, _ := row["user_agent"].(string)
	ip, _ := row["remote_ip"].(string)
	t.Logf("the operator's row: device %q, user_agent %q, remote_ip %q", device, agent, ip)
	switch {
	case strings.TrimSpace(device) != "" || strings.TrimSpace(agent) != "":
		return present
	case strings.TrimSpace(ip) != "":
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
// of one account, gone. It measures by effect — the same account signed in
// from two clients, both locked out after one call, the superuser who made
// the call still signed in — and then the rule the operation carries: a
// request that revokes its own account's sessions keeps the one it was made
// with, or "sign out everywhere else" would sign the operator out of the
// screen they are using.
func probeSessionRevokeAll(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "ops-revoke-all")
	phone := e.signIn(t, op.username, limitedPassword)
	devices := map[string]*http.Client{"laptop": op.client, "phone": phone}
	for name, client := range devices {
		if r := e.request(t, client, http.MethodGet, "/admin/api/health", nil); r.code == http.StatusUnauthorized {
			t.Logf("the %s was not signed in to begin with", name)
			return absent
		}
	}

	r := e.do(t, http.MethodPost, "/admin/api/sessions/revoke-all", map[string]any{"user": op.username})
	if r.code == http.StatusNotFound || r.code == http.StatusMethodNotAllowed || r.servedTheShell() {
		t.Logf("no revoke-all surface: %d (%s)", r.code, r.ctype)
		return absent
	}
	if r.code >= 400 {
		t.Logf("revoke-all answered %d: %s", r.code, r.text())
		return partial
	}
	reported, _ := r.json(t)["revoked"].(float64)
	lockedOut := 0
	for _, client := range devices {
		if e.request(t, client, http.MethodGet, "/admin/api/models", nil).code == http.StatusUnauthorized {
			lockedOut++
		}
	}
	caller := e.get(t, "/admin/api/health")
	t.Logf("revoke-all reported %d revoked; %d of %d devices locked out; the caller answered %d afterwards",
		int(reported), lockedOut, len(devices), caller.code)
	if lockedOut < len(devices) || caller.code != http.StatusOK {
		return partial
	}

	own := e.do(t, http.MethodPost, "/admin/api/sessions/revoke-all", map[string]any{"user": "admin"})
	if own.code >= 400 {
		t.Logf("revoking the caller's own account answered %d: %s", own.code, own.text())
		return partial
	}
	if e.get(t, "/admin/api/health").code != http.StatusOK {
		t.Logf("revoking the caller's own account signed the caller out: %s", own.text())
		return partial
	}
	return present
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
	if r.code != http.StatusOK {
		return absent
	}
	// A 200 is not the control: an empty list with no reason looks exactly
	// like an application whose migrations are all applied. What degrading
	// means here is that the view can say WHY it is empty, so the operator
	// is not left comparing two identical screens.
	body := r.json(t)
	if total, ok := body["total"].(float64); !ok || total != 0 {
		t.Logf("the view claims %v migrations with no directory to read them from", body["total"])
		return partial
	}
	if available, ok := body["available"].(bool); !ok || available {
		t.Logf("the view does not say the directory is missing: %s", r.text())
		return partial
	}
	if message, _ := body["message"].(string); !strings.Contains(message, "migrations") {
		t.Logf("the view is empty without saying why: %s", r.text())
		return partial
	}
	return present
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

// probeCache inspects and empties the cache the application declared.
//
// It runs against the runtime application, not the default one, for the same
// reason OPS-17's sibling probe runs against migrationsApp: a cache is
// something an application HAS, and the default application has none. What
// the default application gets is measured too — see the last block — because
// "there is no cache here" must not be reported as a broken one.
//
// A 200 from the flush is not the control. The old probe accepted any
// non-error answer, and a flush that emptied nothing would have passed it;
// this one puts entries in, reads the count back through the panel, flushes,
// and checks both the count and the cache itself.
func probeCache(t *testing.T, e *env) verdict {
	srv, client, cache := e.runtimeApp(t)
	cache.put("adminbench:one", "1")
	cache.put("adminbench:two", "2")

	stats := requestAs(t, client, srv, http.MethodGet, "/admin/api/cache", nil)
	if stats.code != http.StatusOK {
		t.Logf("cache stats answered %d: %s", stats.code, stats.text())
		return absent
	}
	body := stats.json(t)
	if entries, _ := body["entries"].(float64); entries != 2 {
		t.Logf("the view counts %v entries where the cache holds 2: %s", body["entries"], stats.text())
		return partial
	}
	if canFlush, _ := body["can_flush"].(bool); !canFlush {
		t.Logf("the view does not offer the flush on a cache that has one: %s", stats.text())
		return partial
	}

	flush := requestAs(t, client, srv, http.MethodPost, "/admin/api/cache/flush", map[string]any{})
	if flush.code >= 400 {
		t.Logf("flush answered %d: %s", flush.code, flush.text())
		return partial
	}
	if removed, _ := flush.json(t)["removed"].(float64); removed != 2 {
		t.Logf("the flush reports %v entries removed, not 2: %s", flush.json(t)["removed"], flush.text())
		return partial
	}
	if count, _, _ := cache.CacheEntries(t.Context()); count != 0 {
		t.Logf("the cache still holds %d entries after the panel flushed it", count)
		return partial
	}

	// The default application — no cache of any kind — must say so rather
	// than offer a button that refuses (OR-49).
	bare := e.get(t, "/admin/api/cache")
	if bare.code != http.StatusOK {
		t.Logf("an application with no cache gets %d from the view: %s", bare.code, bare.text())
		return partial
	}
	bareBody := bare.json(t)
	if canFlush, _ := bareBody["can_flush"].(bool); canFlush {
		t.Logf("an application with no cache is still offered the flush: %s", bare.text())
		return partial
	}
	if kind, _ := bareBody["kind"].(string); kind != "none" {
		t.Logf("an application with no cache is reported as %q: %s", kind, bare.text())
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
//
// The first version of this probe looked for the WORDS "queue", "outbox" or
// "sent" anywhere in the payload. That is not a measurement: a view that
// merely names its outbox passes it, and one that reports a queue of the
// wrong size passes it too. It now queues a message and asserts the view
// counts it — against the runtime application, which is the one that has an
// outbox for mail to wait in.
func probeEmailOutbox(t *testing.T, e *env) verdict {
	srv, client, _ := e.runtimeApp(t)

	before := requestAs(t, client, srv, http.MethodGet, "/admin/api/email", nil)
	if before.code != http.StatusOK {
		t.Logf("email answered %d: %s", before.code, before.text())
		return absent
	}
	delivery, ok := before.json(t)["delivery"].(map[string]any)
	if !ok {
		t.Logf("the email view carries no delivery state: %s", before.text())
		return partial
	}
	if enabled, _ := delivery["enabled"].(bool); !enabled {
		t.Logf("the view does not see the outbox this application runs: %s", before.text())
		return partial
	}
	if health, ok := before.json(t)["health"].(map[string]any); !ok || health == nil {
		t.Logf("the view says nothing about whether the sender answers: %s", before.text())
		return partial
	}
	totalBefore, _ := delivery["total"].(float64)

	// One message into the outbox, under the topic mail uses.
	e.queueMail(t, srv)

	after := requestAs(t, client, srv, http.MethodGet, "/admin/api/email", nil)
	if after.code != http.StatusOK {
		t.Logf("email answered %d after queueing: %s", after.code, after.text())
		return partial
	}
	afterDelivery, _ := after.json(t)["delivery"].(map[string]any)

	// The assertion is on the TOTAL, not on the pending count. The
	// dispatcher is running: between the two reads it may have leased the
	// message, so "pending went up by one" is a race the probe would lose
	// at random. A queued message raises the total whichever state it is
	// sitting in, and the per-state counts are asserted below as a set.
	totalAfter, _ := afterDelivery["total"].(float64)
	if totalAfter <= totalBefore {
		t.Logf("the outbox reads %v messages before and %v after one was queued: %s",
			totalBefore, totalAfter, after.text())
		return partial
	}
	queued, _ := afterDelivery["queued"].(float64)
	processing, _ := afterDelivery["processing"].(float64)
	failed, _ := afterDelivery["failed"].(float64)
	delivered, _ := afterDelivery["delivered"].(float64)
	if queued+processing+failed+delivered < 1 {
		t.Logf("the view totals %v messages and accounts for none of them by state: %s",
			totalAfter, after.text())
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
	// This read used to ask for payload["queues"], and the answer has never
	// had a top-level "queues" key — it is {enabled, redis_url, snapshot}. So
	// the branch below was unreachable and the probe measured one thing: that
	// the endpoint answers 200. It did, for the whole of A6, while the panel
	// had no inspector at all and the view it draws was blind (OR-53). The
	// verdict was right for the wrong reason, which no test catches.
	var payload struct {
		Enabled  bool `json:"enabled"`
		Snapshot struct {
			Enabled     bool `json:"enabled"`
			TotalQueues int  `json:"total_queues"`
			Queues      []struct {
				Name string `json:"name"`
			} `json:"queues"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(list.body, &payload); err != nil {
		t.Logf("the jobs answer does not parse: %v: %s", err, list.text())
		return partial
	}
	if !payload.Enabled || !payload.Snapshot.Enabled {
		t.Logf("the panel reports jobs disabled in an application that runs them: %s", list.text())
		return partial
	}
	if len(payload.Snapshot.Queues) == 0 {
		t.Logf("the queue list is empty in an application with a job runtime: %s", list.text())
		return partial
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
// whose session is this. The row has a field for it. The verdict is read
// on the probe's OWN operator's row — it has to carry that operator's
// username, not just something — and every other row has to name somebody
// too, or the viewer would still be blind for part of the list.
func probeSessionOwner(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "ops-owner")
	e.asOperator(t, op, http.MethodGet, "/admin/api/health", nil)
	r := e.get(t, "/admin/api/sessions")
	if r.code != http.StatusOK {
		return absent
	}
	rows, _ := r.json(t)["sessions"].([]any)
	handle := sessionHandleOf(t, e, op)
	named, ownNamed := 0, false
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok {
			continue
		}
		user, _ := entry["user"].(string)
		if strings.TrimSpace(user) != "" {
			named++
		}
		if id, _ := entry["id"].(string); id == handle {
			ownNamed = user == op.username
		}
	}
	t.Logf("%d of %d session rows name their operator; the probe's own row names it: %v", named, len(rows), ownNamed)
	switch {
	case ownNamed && named == len(rows):
		return present
	case ownNamed || named > 0:
		return partial
	default:
		return absent
	}
}
