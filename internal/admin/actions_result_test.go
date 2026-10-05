// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// answeringPanel is a panel with one record action whose answer the test
// writes: the action's function is the test's, not a recorder.
func answeringPanel(t *testing.T, answer func(ActionRequest) ActionResult) (*Panel, *httptest.Server, string) {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	panel.rbac = enf
	panel.audit = newAuditStore(100)
	table, err := validateModelActions([]ModelAction{{
		Name: "answer", Model: "AdminUser", Label: "Answer", Placement: ActionOnSelectionAndRecord,
		Run: func(_ context.Context, req ActionRequest) (ActionResult, error) { return answer(req), nil },
	}}, modelResolver(panel.src))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	panel.modelActions = table
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	created := createAdminUser(t, srv.URL, map[string]interface{}{
		"email": "answer@example.com", "name": "Answer", "active": true,
	})
	return panel, srv, fmt.Sprint(created.ID)
}

// lastActionEntry is the newest audit entry of the "answer" action.
func lastActionEntry(t *testing.T, panel *Panel) AuditEntry {
	t.Helper()
	for _, e := range panel.audit.list(auditQueryOpts{PageSize: 200}) {
		if e.Action == "action.answer" {
			return e
		}
	}
	t.Fatal("the trail has no entry for the action")
	return AuditEntry{}
}

// post sends a record action and returns the raw answer.
func postRaw(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// TestActionAnswersWithARedirectInsideThePanel: a path relative to the panel
// reaches the client as the page to go to, and the trail records that the
// action answered with one, and which.
func TestActionAnswersWithARedirectInsideThePanel(t *testing.T) {
	panel, srv, id := answeringPanel(t, func(req ActionRequest) ActionResult {
		return ActionResult{Message: "copied", Redirect: "/data-studio?model=AdminUser&record=" + req.IDs[0]}
	})
	resp, status := doJSON(t, http.MethodPost, recordActionURL(srv.URL, id, "answer"), nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, resp)
	}
	want := "/data-studio?model=AdminUser&record=" + id
	if resp["result"] != "redirect" || resp["redirect"] != want || resp["message"] != "copied" {
		t.Fatalf("the answer does not carry the redirect: %v", resp)
	}
	entry := lastActionEntry(t, panel)
	if entry.NewValue["result"] != "redirect" || entry.NewValue["redirect"] != want {
		t.Fatalf("the trail does not record the redirect: %+v", entry.NewValue)
	}
}

// TestActionRedirectOutOfThePanelIsRefused: every way a browser can be
// taken off the panel by a "path" is refused when the action answers — with
// a message that says the action ran — and the trail keeps what it tried.
func TestActionRedirectOutOfThePanelIsRefused(t *testing.T) {
	for _, target := range []string{
		"https://evil.example/login",
		"//evil.example/login",
		`/\evil.example`,
		"javascript:alert(1)",
		"evil.example/login",
		"/a/../../elsewhere",
		"/a/%2e%2e/%2e%2e/elsewhere",
		"/\t/evil.example",
	} {
		t.Run(target, func(t *testing.T) {
			panel, srv, id := answeringPanel(t, func(ActionRequest) ActionResult {
				return ActionResult{Redirect: target}
			})
			resp, status := doJSON(t, http.MethodPost, recordActionURL(srv.URL, id, "answer"), nil)
			if status != http.StatusInternalServerError {
				t.Fatalf("a redirect to %q answered %d: %v", target, status, resp)
			}
			text := mustJSON(resp)
			if !strings.Contains(text, "ACTION_ANSWER_REFUSED") || !strings.Contains(text, "Answer ran, and its answer was refused") {
				t.Fatalf("the refusal does not say the action ran and its answer was refused: %s", text)
			}
			if _, leaked := resp["redirect"]; leaked {
				t.Fatalf("the refused redirect reached the client: %v", resp)
			}
			entry := lastActionEntry(t, panel)
			if entry.NewValue["result"] != "redirect" || entry.NewValue["error"] == nil || entry.NewValue["ran"] != true {
				t.Fatalf("the trail does not record the refused redirect: %+v", entry.NewValue)
			}
		})
	}
}

// TestPanelRedirect is the rule on its own, both ways.
func TestPanelRedirect(t *testing.T) {
	for _, ok := range []string{"/", "/data-studio", "/data-studio?model=Note&record=7", "/x/reports/batch/7#totals", "/x/reports/a%20b", " /audit "} {
		if _, err := panelRedirect(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "data-studio", "//evil", "///evil", `/\evil`, `\\evil`, "http://evil", "https:/evil", "mailto:x@y", "/a/./b", "/..", "/a/%2E%2E", "/a%2f..%2fb", "/x\n/y", "/x\x7f"} {
		if _, err := panelRedirect(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// closeCounter is a body that knows whether it was closed.
type closeCounter struct {
	io.Reader
	closed *atomic.Int32
}

func (c closeCounter) Close() error { c.closed.Add(1); return nil }

// TestActionAnswersWithADownload: the file reaches the client as an
// attachment, under the name, the type and the size the action declared,
// with the headers that keep a browser from treating it as a page — and the
// trail records the file.
func TestActionAnswersWithADownload(t *testing.T) {
	var closed atomic.Int32
	panel, srv, id := answeringPanel(t, func(req ActionRequest) ActionResult {
		return ActionResult{Message: "not shown", Download: &ActionDownload{
			Filename:    "exports/../reports/Résumé \"Q3\".txt",
			ContentType: "text/plain; charset=utf-8",
			Body:        closeCounter{Reader: strings.NewReader("record " + req.IDs[0]), closed: &closed},
		}}
	})
	resp, body := postRaw(t, recordActionURL(srv.URL, id, "answer"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if string(body) != "record "+id {
		t.Fatalf("the body is not the action's content: %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
	disposition, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil || disposition != "attachment" {
		t.Fatalf("Content-Disposition %q is not an attachment (%v)", resp.Header.Get("Content-Disposition"), err)
	}
	// Go's parser prefers filename* and decodes it: the exact name, without
	// the directories the action put in front of it.
	if params["filename"] != `Résumé "Q3".txt` {
		t.Fatalf("the saved name is %q", params["filename"])
	}
	raw := resp.Header.Get("Content-Disposition")
	if !strings.Contains(raw, `filename="R_sum_ _Q3_.txt"`) || strings.Contains(raw, "..") || strings.Contains(raw, "/") {
		t.Fatalf("the ASCII fallback is not a safe base name: %s", raw)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff", "Cache-Control": "no-store",
		"Content-Security-Policy": "default-src 'none'; sandbox", "Content-Length": fmt.Sprint(len(body)),
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if closed.Load() != 1 {
		t.Fatalf("the action's body was closed %d times", closed.Load())
	}
	entry := lastActionEntry(t, panel)
	file, _ := entry.NewValue["download"].(map[string]any)
	if entry.NewValue["result"] != "download" || file == nil || file["filename"] != `Résumé "Q3".txt` ||
		file["content_type"] != "text/plain; charset=utf-8" || fmt.Sprint(file["bytes"]) != fmt.Sprint(len(body)) {
		t.Fatalf("the trail does not record the file: %+v", entry.NewValue)
	}
}

// TestActionDownloadIsRefusedWhenThePanelWillNotSendIt: a file over the
// ceiling is refused whole, never sent truncated; a file with no type, no
// name or a control character in its name is refused; an answer that is
// both a redirect and a download is refused. Every body is closed.
func TestActionDownloadIsRefusedWhenThePanelWillNotSendIt(t *testing.T) {
	previous := actionDownloadMaxBytes
	actionDownloadMaxBytes = 16
	t.Cleanup(func() { actionDownloadMaxBytes = previous })

	cases := map[string]struct {
		result func(io.Reader) ActionResult
		says   string
	}{
		"over the ceiling": {func(b io.Reader) ActionResult {
			return ActionResult{Download: &ActionDownload{Filename: "big.bin", ContentType: "application/octet-stream", Body: b}}
		}, "larger than"},
		"no content type": {func(b io.Reader) ActionResult {
			return ActionResult{Download: &ActionDownload{Filename: "a.txt", Body: b}}
		}, "ContentType"},
		"a content type that is not one": {func(b io.Reader) ActionResult {
			return ActionResult{Download: &ActionDownload{Filename: "a.txt", ContentType: "text/plain\r\nSet-Cookie: x=1", Body: b}}
		}, "not a media type"},
		"no name": {func(b io.Reader) ActionResult {
			return ActionResult{Download: &ActionDownload{Filename: "reports/", ContentType: "text/plain", Body: b}}
		}, "names no file"},
		"a control character in the name": {func(b io.Reader) ActionResult {
			return ActionResult{Download: &ActionDownload{Filename: "a\r\nb.txt", ContentType: "text/plain", Body: b}}
		}, "control character"},
		"a redirect and a download": {func(b io.Reader) ActionResult {
			return ActionResult{Redirect: "/audit", Download: &ActionDownload{Filename: "a.txt", ContentType: "text/plain", Body: b}}
		}, "not both"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			var closed atomic.Int32
			body := closeCounter{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 64)), closed: &closed}
			panel, srv, id := answeringPanel(t, func(ActionRequest) ActionResult { return tc.result(body) })
			resp, raw := postRaw(t, recordActionURL(srv.URL, id, "answer"))
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("answered %d: %s", resp.StatusCode, raw)
			}
			if resp.Header.Get("Content-Disposition") != "" || bytes.Contains(raw, []byte("xxxx")) {
				t.Fatalf("a refused download sent something: %s", raw)
			}
			if !strings.Contains(string(raw), tc.says) {
				t.Fatalf("the refusal does not say %q: %s", tc.says, raw)
			}
			if closed.Load() != 1 {
				t.Fatalf("the action's body was closed %d times", closed.Load())
			}
			if entry := lastActionEntry(t, panel); entry.NewValue["result"] != "download" || entry.NewValue["error"] == nil {
				t.Fatalf("the trail does not record the refused download: %+v", entry.NewValue)
			}
		})
	}
}

// TestActionDownloadFromASelection: the answers are not the record view's
// alone — "export these invoices as PDF" is a download over a selection.
func TestActionDownloadFromASelection(t *testing.T) {
	_, srv, id := answeringPanel(t, func(req ActionRequest) ActionResult {
		return ActionResult{Download: &ActionDownload{
			Filename: "selection.csv", ContentType: "text/csv",
			Body: strings.NewReader(strings.Join(req.IDs, ",")),
		}}
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/models/AdminUser/bulk",
		strings.NewReader(`{"action":"answer","ids":["`+id+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != id ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("a download over a selection answered %d %q %q", resp.StatusCode, resp.Header.Get("Content-Disposition"), body)
	}
}

// TestActionMessageNamesItsKind: an action that answers as every action
// did before still does, and the answer now says which kind it is.
func TestActionMessageNamesItsKind(t *testing.T) {
	panel, srv, id := answeringPanel(t, func(ActionRequest) ActionResult {
		return ActionResult{Message: "said", Data: map[string]any{"k": "v"}}
	})
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "answer", "ids": []interface{}{id},
	})
	if status != http.StatusOK || resp["result"] != "message" || resp["message"] != "said" {
		t.Fatalf("status=%d body=%v", status, resp)
	}
	if _, has := resp["redirect"]; has {
		t.Fatalf("a message answer carries a redirect: %v", resp)
	}
	entry := lastActionEntry(t, panel)
	if entry.NewValue["result"] != "message" || entry.NewValue["on"] != "selection" || entry.RecordID != "" {
		t.Fatalf("the trail does not record a selection's message: %+v", entry)
	}
}
