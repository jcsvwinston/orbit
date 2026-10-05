// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	"github.com/jcsvwinston/orbit"
)

// What an application adds to the panel, as the bench's application adds it
// (S7): one action of its own on one of its models, and one screen of its
// own. Both are declared the way an author writes them — through
// orbit.Config, with a plain function and a plain http.Handler — so the
// probes measure the product's extension points and not a hook the bench
// reached in and installed.

// extensions holds what the probed application needs to run its own action:
// the database handle, taken from the runtime in OnStart, and the last call
// the action received, so a probe can ask what the panel told it.
type extensions struct {
	mu       sync.Mutex
	db       *sql.DB
	lastCall orbit.ActionRequest
	calls    int
}

// benchExtensions is the application's, not the bench's: the module below
// fills it at startup and the action closes over it, which is what an
// application does with its own dependencies.
var benchExtensions = &extensions{}

// extensionsModule lends the application's database handle to the action and
// the widget this application declares, and serves the logo its branding
// names. It is mounted only on the shared bench application (env_test.go).
//
// The logo is served because a declared logo the application does not serve
// is a broken image, and a broken image is not a drawn logo: UIX-07 asks the
// browser whether the image LOADED on the login screen, which is drawn before
// anyone has signed in. Until A11 O1 the bench declared this path and served
// nothing at it, and every reading of "the logo is drawn" was a 404.
func extensionsModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   "benchext",
		Prefix: "/static",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			benchExtensions.captureRuntime(rt)
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/bench-logo.svg" {
					http.NotFound(w, req)
					return
				}
				w.Header().Set("Content-Type", "image/svg+xml")
				_, _ = w.Write([]byte(benchLogoSVG))
			}))
		},
	}.Build()
}

// benchLogoSVG is the bench application's logo: a mark with a size, so a
// browser that loaded it reports a natural width.
const benchLogoSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="32" viewBox="0 0 120 32">` +
	`<rect width="120" height="32" rx="6" fill="#0b5fff"/></svg>`

// captureRuntime records the handle the action and the widget read through.
// It runs before any request.
func (x *extensions) captureRuntime(rt nucleus.Runtime) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if h := rt.DatabaseHandle(); h != nil {
		if sqlDB, err := h.SqlDB(); err == nil {
			x.db = sqlDB
		}
	}
}

func (x *extensions) handle() *sql.DB {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.db
}

func (x *extensions) record(req orbit.ActionRequest) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.lastCall = req
	x.calls++
}

func (x *extensions) lastRequest() (orbit.ActionRequest, int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.lastCall, x.calls
}

// publishAction is the "publish these three" every admin product grows: a
// verb the panel cannot infer from a column called status, over the rows the
// operator selected.
func publishAction() orbit.ModelAction {
	return orbit.ModelAction{
		Name:        "publish",
		Model:       "Note",
		Label:       "Publish",
		Description: "Mark the selected notes as published",
		Confirm:     "Publish the selected notes?",
		Destructive: true,
		Run: func(ctx context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			benchExtensions.record(req)
			handle := benchExtensions.handle()
			if handle == nil {
				return orbit.ActionResult{}, fmt.Errorf("no database handle")
			}
			affected := 0
			for _, id := range req.IDs {
				res, err := handle.ExecContext(ctx, "UPDATE notes SET status = 'published' WHERE id = ?", id)
				if err != nil {
					return orbit.ActionResult{Affected: affected}, err
				}
				if n, err := res.RowsAffected(); err == nil {
					affected += int(n)
				}
			}
			return orbit.ActionResult{
				Message:  fmt.Sprintf("%d note(s) published", affected),
				Affected: affected,
				Data:     map[string]any{"status": "published"},
			}, nil
		},
	}
}

// scheduleAction is the action that asks before it runs (EXT-01): the reason,
// the channel, the day — one input of each type the panel draws. It writes
// nothing: what the probes measure is what the panel let through to it and
// in what type, and a row it changed would be one more thing the other
// probes have to step around.
func scheduleAction() orbit.ModelAction {
	return orbit.ModelAction{
		Name:        "schedule",
		Model:       "Note",
		Label:       "Schedule",
		Description: "Schedule the selected notes for publication",
		// Offered on a selection and on one record, so the record
		// endpoint's input check is measured against the same declaration
		// as the bulk one (EXT-02).
		Placement: orbit.ActionOnSelectionAndRecord,
		Fields: []orbit.ActionField{
			{Name: "reason", Label: "Reason", Required: true, Help: "Recorded with the audit entry"},
			{Name: "priority", Label: "Priority", Type: orbit.ActionFieldNumber},
			{Name: "notify", Label: "Notify subscribers", Type: orbit.ActionFieldBoolean},
			{Name: "channel", Label: "Channel", Type: orbit.ActionFieldSelect, Required: true, Options: []orbit.ActionOption{
				{Value: "web", Label: "Website"}, {Value: "email", Label: "Email"},
			}},
			{Name: "publish_on", Label: "Publish on", Type: orbit.ActionFieldDate, Required: true},
		},
		Run: func(_ context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			benchExtensions.record(req)
			day, _ := req.Input.Date("publish_on")
			return orbit.ActionResult{
				Message: fmt.Sprintf("%d note(s) scheduled on %s for %s: %s",
					len(req.IDs), req.Input.String("channel"), day.Format("2006-01-02"), req.Input.String("reason")),
				Affected: len(req.IDs),
			}, nil
		},
	}
}

// duplicateAction is an action about one record that answers with a page of
// the panel (EXT-02, EXT-03): it copies the note and takes the operator to
// the copy's record view, which is "open what this action made" — the most
// ordinary redirect an admin action has.
func duplicateAction() orbit.ModelAction {
	return orbit.ModelAction{
		Name:        "duplicate",
		Model:       "Note",
		Label:       "Duplicate",
		Description: "Copy this note and open the copy",
		Placement:   orbit.ActionOnRecord,
		Run: func(ctx context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			benchExtensions.record(req)
			handle := benchExtensions.handle()
			if handle == nil {
				return orbit.ActionResult{}, fmt.Errorf("no database handle")
			}
			res, err := handle.ExecContext(ctx,
				`INSERT INTO notes (title, body, status, views, cover, meta, created_at, updated_at)
				 SELECT 'Copy of ' || title, body, 'draft', 0, cover, meta, created_at, updated_at FROM notes WHERE id = ?`,
				req.IDs[0])
			if err != nil {
				return orbit.ActionResult{}, err
			}
			copyID, err := res.LastInsertId()
			if err != nil {
				return orbit.ActionResult{}, err
			}
			return orbit.ActionResult{
				Message:  fmt.Sprintf("note %s copied as %d", req.IDs[0], copyID),
				Affected: 1,
				Redirect: fmt.Sprintf("/data-studio?model=Note&record=%d", copyID),
			}, nil
		},
	}
}

// downloadTextAction answers with a file (EXT-03), from one record or from a
// selection: the notes as plain text, named after what it holds.
func downloadTextAction() orbit.ModelAction {
	return orbit.ModelAction{
		Name:        "download_text",
		Model:       "Note",
		Label:       "Download as text",
		Description: "The notes as a text file",
		Placement:   orbit.ActionOnSelectionAndRecord,
		Run: func(ctx context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			benchExtensions.record(req)
			handle := benchExtensions.handle()
			if handle == nil {
				return orbit.ActionResult{}, fmt.Errorf("no database handle")
			}
			var text strings.Builder
			for _, id := range req.IDs {
				var title, body string
				if err := handle.QueryRowContext(ctx, "SELECT title, body FROM notes WHERE id = ?", id).Scan(&title, &body); err != nil {
					return orbit.ActionResult{}, err
				}
				fmt.Fprintf(&text, "%s\n%s\n\n", title, body)
			}
			name := "notes.txt"
			if len(req.IDs) == 1 {
				name = "note-" + req.IDs[0] + ".txt"
			}
			return orbit.ActionResult{Download: &orbit.ActionDownload{
				Filename:    name,
				ContentType: "text/plain; charset=utf-8",
				Body:        strings.NewReader(text.String()),
			}}, nil
		},
	}
}

// reportsPage is the screen an application has that the panel could never
// ship: it writes its own document and names the operator reading it, which
// is what makes it a page of the panel and not a public URL.
const reportsMarker = "bench-extension-report"

func reportsPage() orbit.Page {
	return orbit.Page{
		ID:          "reports",
		Title:       "Bench Reports",
		Description: "A screen this application added",
		Icon:        "chart",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operator, ok := orbit.OperatorFromContext(r.Context())
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// The path the handler sees is its own, so a page can route
			// below its root without knowing where the panel is mounted.
			fmt.Fprintf(w, "<!doctype html><title>%s</title><p>%s</p><p>operator=%s authenticated=%t</p><p>path=%s</p>",
				reportsMarker, reportsMarker, operator.Username, ok, r.URL.Path)
		}),
	}
}

// benchBranding is what a product team asks for on day one: our logo, our
// colour, our icon in the tab.
func benchBranding() orbit.Branding {
	return orbit.Branding{
		LogoURL:      "/static/bench-logo.svg",
		FaviconURL:   "https://example.test/bench.ico",
		PrimaryColor: "#0b5fff",
	}
}

// benchWidgets are the application's own numbers on the panel's landing
// screen: one card that counts something of the application's, and one that
// fails, because a screen that drops a broken card reports a broken query as
// "nothing to see".
func benchWidgets() []orbit.Widget {
	return []orbit.Widget{
		{
			ID:          "pending-notes",
			Title:       "Pending notes",
			Description: "Drafts nobody has published yet",
			Link:        "/data-studio",
			Load: func(ctx context.Context) (orbit.WidgetValue, error) {
				handle := benchExtensions.handle()
				if handle == nil {
					return orbit.WidgetValue{}, fmt.Errorf("no database handle")
				}
				var count int
				if err := handle.QueryRowContext(ctx,
					"SELECT COUNT(*) FROM notes WHERE status <> 'published'").Scan(&count); err != nil {
					return orbit.WidgetValue{}, err
				}
				return orbit.WidgetValue{
					Value:  fmt.Sprintf("%d", count),
					Detail: "since the bench started",
				}, nil
			},
		},
		{
			ID:    "broken-card",
			Title: "Broken card",
			Load: func(context.Context) (orbit.WidgetValue, error) {
				return orbit.WidgetValue{}, fmt.Errorf("this reading is unavailable")
			},
		},
	}
}

// trendsDashboard is the bench application's second screen of cards (A11
// O5): the one an operator granted view on admin:dashboard:trends opens,
// and nobody else sees.
const trendsDashboard = "trends"

// seriesDays is how many days the bench's series covers, today included.
const seriesDays = 7

// benchDashboards declares that screen the way an author writes one: each
// card a kind and the function its kind reads, all of them reading the
// application's own table. Nothing on it is synthetic — the series counts
// the notes a probe creates, so a probe can tell a chart fed by the
// function from a chart drawn from a constant.
func benchDashboards() []orbit.Dashboard {
	return []orbit.Dashboard{{
		ID:          trendsDashboard,
		Title:       "Bench Trends",
		Description: "How the notes move",
		Columns:     2,
		Widgets: []orbit.Widget{
			{
				ID:     "notes-per-day",
				Title:  "Notes per day",
				Kind:   orbit.WidgetLine,
				Span:   2,
				Series: notesPerDay,
			},
			{
				ID:    "published-notes",
				Title: "Published notes",
				Kind:  orbit.WidgetStat,
				Stat:  publishedNotes,
			},
			{
				ID:    "notes-by-status",
				Title: "Notes by status",
				Kind:  orbit.WidgetTable,
				Span:  1,
				Table: notesByStatus,
			},
			{
				ID:      "recent-notes",
				Title:   "Recent notes",
				Kind:    orbit.WidgetRecords,
				Records: orbit.RecordList{Model: "Note", Fields: []string{"title", "status"}, Limit: 5},
			},
		},
	}}
}

// seriesLabels are the last seriesDays days, oldest first, in UTC: the
// day a note's created_at falls on.
func seriesLabels(now time.Time) []string {
	labels := make([]string, seriesDays)
	for i := range labels {
		labels[i] = now.UTC().AddDate(0, 0, i-(seriesDays-1)).Format("2006-01-02")
	}
	return labels
}

// noteDay is the SQL for the UTC day a note was created on. It is a prefix
// of the text and not SQLite's date(): the driver writes a timestamp as Go
// prints one ("2026-10-04 22:30:06.67 +0000 UTC"), which date() does not
// parse — it answers NULL, and the first version of this series was a flat
// line of zeros that the probe caught by creating a note and watching
// nothing move.
const noteDay = "substr(created_at, 1, 10)"

// notesPerDay counts the notes created on each of the last seven days.
func notesPerDay(ctx context.Context) (orbit.SeriesValue, error) {
	handle := benchExtensions.handle()
	if handle == nil {
		return orbit.SeriesValue{}, fmt.Errorf("no database handle")
	}
	labels := seriesLabels(time.Now())
	rows, err := handle.QueryContext(ctx,
		"SELECT "+noteDay+", COUNT(*) FROM notes WHERE deleted_at IS NULL AND "+noteDay+" >= ? GROUP BY "+noteDay,
		labels[0])
	if err != nil {
		return orbit.SeriesValue{}, err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]float64{}
	for rows.Next() {
		var day sql.NullString
		var n float64
		if err := rows.Scan(&day, &n); err != nil {
			return orbit.SeriesValue{}, err
		}
		counts[day.String] = n
	}
	if err := rows.Err(); err != nil {
		return orbit.SeriesValue{}, err
	}
	values := make([]float64, len(labels))
	for i, day := range labels {
		values[i] = counts[day]
	}
	return orbit.SeriesValue{
		Labels: labels,
		Series: []orbit.Series{{Name: "Created", Values: values}},
	}, nil
}

// publishedNotes is a figure and how it moved: the published notes, and
// how many of them were created today.
func publishedNotes(ctx context.Context) (orbit.StatValue, error) {
	handle := benchExtensions.handle()
	if handle == nil {
		return orbit.StatValue{}, fmt.Errorf("no database handle")
	}
	var total, today int
	if err := handle.QueryRowContext(ctx,
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN "+noteDay+" = ? THEN 1 ELSE 0 END), 0) FROM notes WHERE status = 'published' AND deleted_at IS NULL",
		time.Now().UTC().Format("2006-01-02")).Scan(&total, &today); err != nil {
		return orbit.StatValue{}, err
	}
	trend := "flat"
	if today > 0 {
		trend = "up"
	}
	return orbit.StatValue{
		Value:     fmt.Sprintf("%d", total),
		Delta:     fmt.Sprintf("+%d today", today),
		Trend:     trend,
		Sentiment: "good",
	}, nil
}

// notesByStatus is a table the application formats itself.
func notesByStatus(ctx context.Context) (orbit.TableValue, error) {
	handle := benchExtensions.handle()
	if handle == nil {
		return orbit.TableValue{}, fmt.Errorf("no database handle")
	}
	rows, err := handle.QueryContext(ctx,
		"SELECT COALESCE(NULLIF(status, ''), '(none)'), COUNT(*) FROM notes WHERE deleted_at IS NULL GROUP BY 1 ORDER BY 1 LIMIT 20")
	if err != nil {
		return orbit.TableValue{}, err
	}
	defer func() { _ = rows.Close() }()
	table := orbit.TableValue{Columns: []string{"Status", "Notes"}}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return orbit.TableValue{}, err
		}
		table.Rows = append(table.Rows, []string{status, fmt.Sprintf("%d", n)})
	}
	return table, rows.Err()
}

// benchMessages is an application adding to the panel's own phrases — and
// translating one of its own words, in a locale the panel ships.
func benchMessages() map[string]map[string]string {
	return map[string]map[string]string{
		"es": {"dashboard.widgets": "Tu aplicación del banco"},
	}
}

// benchOrbitConfig is the mounted panel's configuration, in one place so
// every probe that boots its own application declares the same extension
// points as the shared one.
func benchOrbitConfig() orbit.Config {
	return orbit.Config{
		Prefix:            "/admin",
		Title:             "Admin Bench",
		BootstrapUsername: "admin",
		BootstrapEmail:    "admin@example.test",
		BootstrapPassword: bootstrapPassword,
		RowOwnerFields:    map[string]string{"Article": "owner"},
		FieldWidgets: map[string]string{
			"Note.Body":  "richtext",
			"Note.Cover": "image",
			"Note.Meta":  "json",
			// Drawn by the application's own renderer (EXT-07), which its
			// own script registers (EXT-06).
			"Note.Status": noteStatusRenderer,
		},
		Actions:    []orbit.ModelAction{publishAction(), scheduleAction(), duplicateAction(), downloadTextAction()},
		Pages:      []orbit.Page{reportsPage()},
		Branding:   benchBranding(),
		Widgets:    benchWidgets(),
		Dashboards: benchDashboards(),
		Client:     benchClient(),
		Locale:     "es",
		Messages:   benchMessages(),
	}
}

// The application's own client code (A11 O6: EXT-06, EXT-07, UIX-12), as an
// application ships it: files embedded in its binary, declared by path, and
// the renderer its script registers. client/ also holds a file the
// application never declares, which the panel must not serve.
//
//go:embed client
var benchClientFS embed.FS

// noteStatusRenderer is the renderer client/note-status.js registers: a
// note's status as a badge, for the statuses it knows, and a throw for any
// other.
const noteStatusRenderer = "note-status"

func benchClientFiles() fs.FS {
	files, err := fs.Sub(benchClientFS, "client")
	if err != nil {
		panic(err)
	}
	return files
}

func benchClient() orbit.ClientCode {
	return orbit.ClientCode{
		Files:          benchClientFiles(),
		Scripts:        []string{"note-status.js"},
		Stylesheets:    []string{"note-status.css"},
		FieldRenderers: []string{noteStatusRenderer},
	}
}

// pageURLFor finds a declared page in the navigation payload.
func pageURLFor(payload map[string]any, id string) (string, bool) {
	pages, _ := payload["pages"].([]any)
	for _, entry := range pages {
		page, _ := entry.(map[string]any)
		if page == nil {
			continue
		}
		if fmt.Sprintf("%v", page["id"]) != id {
			continue
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", page["url"]))
		return url, url != ""
	}
	return "", false
}
