// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
)

// auditEntries reads the log the panel serves and returns it as a slice.
func auditEntries(t *testing.T, e *env) []map[string]any {
	t.Helper()
	r := e.get(t, "/admin/api/audit?page_size=200")
	if r.code != http.StatusOK {
		t.Logf("GET /admin/api/audit answered %d: %s", r.code, r.text())
		return nil
	}
	payload := r.json(t)
	raw, _ := payload["items"].([]any)
	if raw == nil {
		raw, _ = payload["entries"].([]any)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

// probeAuditWrites checks that a write to a record leaves a trace naming who,
// what and which row.
func probeAuditWrites(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "audited", "status": "audit"})
	for _, entry := range auditEntries(t, e) {
		if entry["action"] == "create" && entry["model_name"] == "Note" && entry["record_id"] == id {
			if entry["username"] == "" || entry["username"] == nil {
				t.Logf("the entry does not name the operator: %v", entry)
				return partial
			}
			return present
		}
	}
	t.Logf("no create entry for Note %s", id)
	return absent
}

// probeAuditCoverage leaves the Data Studio and acts on the management
// surfaces. An audit log that only covers the CRUD pages cannot answer "who
// flushed the cache" or "who changed that policy".
func probeAuditCoverage(t *testing.T, e *env) verdict {
	e.do(t, http.MethodPost, "/admin/api/system/flags",
		map[string]any{"name": "adminbench_flag", "enabled": true})
	e.do(t, http.MethodPost, "/admin/api/rbac/policies",
		map[string]any{"sub": "adminbench-audit", "obj": "admin:Note", "act": "list"})
	// The third surface is the live feed's exclude list, not the cache: an
	// application without Redis answers 400 to a flush, and an action that
	// never happened is not evidence that nothing audits it.
	e.do(t, http.MethodPost, "/admin/api/live/excludes", map[string]any{"pattern": "/adminbench"})

	want := map[string]bool{"flag": false, "rbac": false, "live": false}
	for _, entry := range auditEntries(t, e) {
		action, _ := entry["action"].(string)
		switch {
		case strings.HasPrefix(action, "flag"):
			want["flag"] = true
		case strings.HasPrefix(action, "rbac"):
			want["rbac"] = true
		case strings.HasPrefix(action, "live"):
			want["live"] = true
		}
	}
	missing := []string{}
	for surface, seen := range want {
		if !seen {
			missing = append(missing, surface)
		}
	}
	if len(missing) > 0 {
		t.Logf("management surfaces with no audit entry: %v", missing)
		return partial
	}
	return present
}

// probeAuditValues asks what an audit log is for: what the row said before
// the edit, and what it says now.
func probeAuditValues(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "before", "status": "values"})
	e.do(t, http.MethodPut, "/admin/api/models/Note/"+id, map[string]any{"title": "after"})

	for _, entry := range auditEntries(t, e) {
		if entry["action"] != "update" || entry["record_id"] != id {
			continue
		}
		oldValue, _ := entry["old_value"].(map[string]any)
		newValue, _ := entry["new_value"].(map[string]any)
		switch {
		case len(oldValue) > 0 && len(newValue) > 0:
			return present
		case len(newValue) > 0 || len(oldValue) > 0:
			t.Logf("the update entry carries only one side: %v", entry)
			return partial
		default:
			t.Logf("the update entry carries neither side: %v", entry)
			return absent
		}
	}
	t.Logf("no update entry for Note %s", id)
	return absent
}

// probeAuditRedaction writes a field whose name says it is a secret and reads
// the log back. An audit trail that copies passwords into a buffer any
// operator can list is a new place to leak them.
func probeAuditRedaction(t *testing.T, e *env) verdict {
	const secret = "adminbench-plaintext-secret"
	r := e.do(t, http.MethodPost, "/admin/api/models/Credential",
		map[string]any{"label": "redaction", "password": secret, "api_token": secret})
	if r.code >= 400 {
		t.Logf("create Credential answered %d: %s", r.code, r.text())
		return absent
	}
	for _, entry := range auditEntries(t, e) {
		if entry["model_name"] != "Credential" {
			continue
		}
		if strings.Contains(entryText(entry), secret) {
			t.Logf("the audit entry holds the secret in clear: %v", entry)
			return absent
		}
		return present
	}
	t.Logf("no audit entry for Credential at all")
	return absent
}

// probeAuditPersistence boots a SECOND application on the same database and
// asks it for the trail the first one recorded. Nothing else answers the
// question an incident asks: what happened before the restart.
func probeAuditPersistence(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "survives-restart", "status": "persist"})

	cfg := benchConfig(t)
	cfg.Databases = e.dbs // the same storage, so the question is what survived
	second := nucleustest.StartApp(t, nucleus.App{
		Config: cfg,
		Modules: map[string]nucleus.ModuleSpec{
			"content": contentModule(),
			"orbit": orbit.Module(orbit.Config{
				Prefix:            "/admin",
				Title:             "Admin Bench (restarted)",
				BootstrapUsername: "admin",
				BootstrapEmail:    "admin@example.test",
				BootstrapPassword: bootstrapPassword,
			}),
		},
	})

	client := signInTo(t, second, "admin", bootstrapPassword)
	req, err := http.NewRequest(http.MethodGet, second.URL("/admin/api/audit?page_size=200"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("read audit from the restarted application: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload map[string]any
	decodeInto(t, resp.Body, &payload)

	// Look for THE entry — action, model and record id — not for the id
	// anywhere in the payload. A record id is a short number: asking whether
	// "7" appears in the new process's own trail (its login, its timestamps)
	// answers yes for reasons that have nothing to do with persistence, and
	// this probe reported the trail as surviving on exactly that.
	raw, _ := payload["items"].([]any)
	if raw == nil {
		raw, _ = payload["entries"].([]any)
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if entry["action"] == "create" && entry["model_name"] == "Note" && entry["record_id"] == id {
			return present
		}
	}
	t.Logf("the record written before the restart (Note %s) has no create entry in the new process's trail (%d entries): the log is a process-lifetime buffer",
		id, len(raw))
	return absent
}

// probeAuditExport asks for the trail as a file, which is what a compliance
// request actually asks for.
func probeAuditExport(t *testing.T, e *env) verdict {
	csv := e.get(t, "/admin/api/audit?format=csv&page_size=10")
	if csv.code == http.StatusOK && !csv.servedTheShell() && strings.HasPrefix(strings.TrimSpace(csv.raw()), "id,") {
		return present
	}
	return e.unrouted(t, "/admin/api/audit/export", "/admin/api/audit/download")
}

// probeAuditRetention asks whether the operator can say how long the trail is
// kept. The only knob is a ring size in the mount config, and the panel does
// not serve a retention surface at all.
func probeAuditRetention(t *testing.T, e *env) verdict {
	if v := e.unrouted(t, "/admin/api/audit/retention", "/admin/api/audit/settings"); v != absent {
		return v
	}
	if configHasKey("audit_retention") || configHasKey("audit_retention_days") {
		return partial
	}
	if configHasKey("audit_max_size") {
		t.Logf("the only bound on the trail is the ring size (audit_max_size), which is a count, not a period")
		return partial
	}
	return absent
}
