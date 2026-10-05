// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// The kinds of card and the dashboards beside the overview (A11 O5).

func valueLoad(v string) func(context.Context) (WidgetValue, error) {
	return func(context.Context) (WidgetValue, error) { return WidgetValue{Value: v}, nil }
}

func seriesOf(labels []string, series ...Series) func(context.Context) (SeriesValue, error) {
	return func(context.Context) (SeriesValue, error) {
		return SeriesValue{Labels: labels, Series: series}, nil
	}
}

// dashboardPanel is a panel with the given overview widgets, dashboards and
// RBAC, served over HTTP. The superuser is signed in until the test changes
// provider.user.
func dashboardPanel(t *testing.T, widgets []Widget, dashboards []Dashboard, policies ...[3]string) (*Panel, *testAdminAuth, string) {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	for _, pol := range policies {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatalf("AddPolicy: %v", err)
		}
	}
	panel.rbac = enf

	compiled, err := validateWidgetList("widgets", widgets, lookupIn(panel.src), maxGridColumns, true)
	if err != nil {
		t.Fatalf("validate widgets: %v", err)
	}
	panel.widgets = compiled
	boards, err := validateDashboards(dashboards, lookupIn(panel.src))
	if err != nil {
		t.Fatalf("validate dashboards: %v", err)
	}
	panel.dashboards = boards

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, provider, srv.URL
}

func rawGet(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw), resp.StatusCode
}

// TestValueWidgetPayloadIsTheOneA6Served: an overview declared before the
// kinds existed is served byte for byte as it was — QADR-0010, a panel
// declared today renders exactly as today.
func TestValueWidgetPayloadIsTheOneA6Served(t *testing.T) {
	_, _, srv := dashboardPanel(t, []Widget{
		{ID: "orders", Title: "Orders", Description: "awaiting review", Link: "/data-studio", Load: func(context.Context) (WidgetValue, error) {
			return WidgetValue{Value: "12", Detail: "oldest: 3 days", Items: []WidgetItem{{Label: "a", Value: "1"}}}, nil
		}},
	}, nil)
	body, status := rawGet(t, srv+"/api/ui/dashboard")
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d", status)
	}
	want := `{"widgets":[{"id":"orders","title":"Orders","description":"awaiting review","link":"/data-studio","value":"12","detail":"oldest: 3 days","items":[{"label":"a","value":"1"}]}]}`
	if strings.TrimSpace(body) != want {
		t.Fatalf("the A6 payload changed:\n got %s\nwant %s", strings.TrimSpace(body), want)
	}

	// And the navigation of an application that declared no dashboard is
	// the one it was: pages, and nothing else.
	nav, status := rawGet(t, srv+"/api/ui/extensions")
	if status != http.StatusOK || strings.TrimSpace(nav) != `{"pages":[]}` {
		t.Fatalf("the navigation payload changed (%d): %s", status, nav)
	}
}

// TestWidgetKindsAreRefusedAtStartupByName covers every declaration a card
// could not draw: each is a card that would otherwise never appear, or
// appear drawing something other than what its author wrote.
func TestWidgetKindsAreRefusedAtStartupByName(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	t.Cleanup(cleanup)
	models := lookupIn(panel.src)

	line := seriesOf([]string{"a"}, Series{Values: []float64{1}})
	cases := []struct {
		name string
		in   Widget
		want []string
	}{
		{"an unknown kind", Widget{ID: "pie", Kind: "pie", Series: line}, []string{"widgets[0] (pie)", `Kind "pie"`}},
		{"a line with no series", Widget{ID: "signups", Kind: "line"}, []string{"widgets[0] (signups)", `kind "line" needs Series`}},
		{"a stat with no stat", Widget{ID: "revenue", Kind: "stat", Load: valueLoad("1")}, []string{"(revenue)", "needs Stat"}},
		{"a value with no load", Widget{ID: "cards"}, []string{"(cards)", "Load is required"}},
		{"a series on a stat", Widget{ID: "mixed", Kind: "stat", Stat: func(context.Context) (StatValue, error) { return StatValue{}, nil }, Series: line}, []string{"(mixed)", "Series is set"}},
		{"a load on a chart", Widget{ID: "both", Kind: "bar", Series: line, Load: valueLoad("1")}, []string{"(both)", "Load is set"}},
		{"a table with no table", Widget{ID: "rows", Kind: "table"}, []string{"(rows)", "needs Table"}},
		{"records with no model", Widget{ID: "recent", Kind: "records"}, []string{"(recent)", "needs Records.Model"}},
		{"records of an unknown model", Widget{ID: "recent", Kind: "records", Records: RecordList{Model: "Invoice"}}, []string{"(recent)", `no model named "Invoice"`}},
		{"records of an unknown field", Widget{ID: "recent", Kind: "records", Records: RecordList{Model: "AdminUser", Fields: []string{"nickname"}}}, []string{"(recent)", `no field "nickname"`}},
		{"records in an order the grid refuses", Widget{ID: "recent", Kind: "records", Records: RecordList{Model: "AdminUser", OrderBy: "name sideways"}}, []string{"(recent)", "Records.OrderBy"}},
		{"records beyond the limit", Widget{ID: "recent", Kind: "records", Records: RecordList{Model: "AdminUser", Limit: 500}}, []string{"(recent)", "Records.Limit 500"}},
		{"records declared on a value card", Widget{ID: "stray", Load: valueLoad("1"), Records: RecordList{Model: "AdminUser"}}, []string{"(stray)", "Records is set"}},
		{"a span wider than the screen", Widget{ID: "wide", Load: valueLoad("1"), Span: 5}, []string{"(wide)", "Span 5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateWidgetList("widgets", []Widget{tc.in}, models, maxGridColumns, true)
			if err == nil {
				t.Fatalf("accepted: %+v", tc.in)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal does not say %q: %v", want, err)
				}
			}
		})
	}

	// What the panel can draw starts — including "value" spelled out, and
	// a kind in capitals.
	ok := []Widget{
		{ID: "v", Kind: "value", Load: valueLoad("1")},
		{ID: "s", Kind: "STAT", Stat: func(context.Context) (StatValue, error) { return StatValue{}, nil }},
		{ID: "l", Kind: "line", Series: line},
		{ID: "b", Kind: "bar", Series: line, Span: 4},
		{ID: "t", Kind: "table", Table: func(context.Context) (TableValue, error) { return TableValue{}, nil }},
		{ID: "r", Kind: "records", Records: RecordList{Model: "AdminUser", Fields: []string{"email"}, OrderBy: "created_at desc", Limit: 3}},
	}
	compiled, err := validateWidgetList("widgets", ok, models, maxGridColumns, false)
	if err != nil {
		t.Fatalf("a drawable declaration was refused: %v", err)
	}
	if compiled[0].Kind != "" || compiled[1].Kind != WidgetKindStat || compiled[5].Records.Fields[0] != "Email" {
		t.Fatalf("the kinds were not normalized: %+v", compiled)
	}
	// A chart declared with no span takes two columns, and never more than
	// its screen has.
	if compiled[2].Span != 2 || compiled[0].Span != 0 {
		t.Fatalf("default spans: line %d, value %d", compiled[2].Span, compiled[0].Span)
	}
	narrow, err := validateWidgetList("widgets", []Widget{{ID: "l", Kind: "line", Series: line}}, models, 1, false)
	if err != nil || narrow[0].Span != 1 {
		t.Fatalf("a chart on a one-column screen: %+v %v", narrow, err)
	}
}

// TestDashboardsAreRefusedAtStartupByName: the screen-level mistakes.
func TestDashboardsAreRefusedAtStartupByName(t *testing.T) {
	line := seriesOf([]string{"a"}, Series{Values: []float64{1}})
	card := Widget{ID: "signups", Kind: "line", Series: line}
	cases := []struct {
		name string
		in   []Dashboard
		want []string
	}{
		{"no cards", []Dashboard{{ID: "finance"}}, []string{"dashboards[0] (finance)", "at least one widget"}},
		{"an id with a slash", []Dashboard{{ID: "a/b", Widgets: []Widget{card}}}, []string{"dashboards[0]", "lowercase"}},
		{"two of one id", []Dashboard{{ID: "a", Widgets: []Widget{card}}, {ID: "A", Widgets: []Widget{card}}}, []string{"dashboards[1]", "already declared"}},
		{"too many columns", []Dashboard{{ID: "a", Columns: 6, Widgets: []Widget{card}}}, []string{"(a)", "Columns 6"}},
		{"a card wider than its dashboard", []Dashboard{{ID: "a", Columns: 1, Widgets: []Widget{{ID: "signups", Kind: "line", Series: line, Span: 2}}}}, []string{"dashboards[0] (a).widgets[0] (signups)", "Span 2"}},
		{"a card of an unknown kind", []Dashboard{{ID: "a", Widgets: []Widget{{ID: "x", Kind: "gauge"}}}}, []string{"dashboards[0] (a).widgets[0] (x)", `Kind "gauge"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateDashboards(tc.in, nil)
			if err == nil {
				t.Fatalf("accepted: %+v", tc.in)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal does not say %q: %v", want, err)
				}
			}
		})
	}
}

// TestNewKindsReachTheScreenAndBreakAlone: each kind travels as the card
// draws it, and a function that returns what its card cannot draw — a
// series shorter than its axis, a value that is not a number, a row with a
// missing cell, an unknown trend — is that card's error, never the screen's.
func TestNewKindsReachTheScreenAndBreakAlone(t *testing.T) {
	days := []string{"Mon", "Tue", "Wed"}
	_, _, srv := dashboardPanel(t, []Widget{
		{ID: "a-revenue", Kind: "stat", Stat: func(context.Context) (StatValue, error) {
			return StatValue{Value: "$1.2M", Delta: "+12%", Trend: "Up", Sentiment: "good"}, nil
		}},
		{ID: "b-signups", Kind: "line", Series: seriesOf(days,
			Series{Name: "Web", Values: []float64{1, 2, 3}}, Series{Name: "Mobile", Values: []float64{0, 1, 5}})},
		{ID: "c-queue", Kind: "table", Table: func(context.Context) (TableValue, error) {
			return TableValue{Columns: []string{"Queue", "Depth"}, Rows: [][]string{{"mail", "3"}}}, nil
		}},
		{ID: "d-short", Kind: "bar", Series: seriesOf(days, Series{Name: "x", Values: []float64{1}})},
		{ID: "e-nan", Kind: "line", Series: seriesOf(days, Series{Values: []float64{1, math.NaN(), 3}})},
		{ID: "f-inf", Kind: "line", Series: seriesOf(days, Series{Values: []float64{1, math.Inf(1), 3}})},
		{ID: "g-ragged", Kind: "table", Table: func(context.Context) (TableValue, error) {
			return TableValue{Columns: []string{"a", "b"}, Rows: [][]string{{"1"}}}, nil
		}},
		{ID: "h-trend", Kind: "stat", Stat: func(context.Context) (StatValue, error) {
			return StatValue{Value: "1", Delta: "+1", Trend: "sideways"}, nil
		}},
		{ID: "i-panics", Kind: "bar", Series: func(context.Context) (SeriesValue, error) { panic("nil map") }},
		{ID: "j-fails", Kind: "table", Table: func(context.Context) (TableValue, error) {
			return TableValue{}, fmt.Errorf("the warehouse is down")
		}},
		{ID: "k-unnamed", Kind: "line", Series: seriesOf(days, Series{Values: []float64{1, 2, 3}}, Series{Values: []float64{1, 2, 3}})},
	}, nil)

	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboard", nil)
	if status != http.StatusOK {
		t.Fatalf("one bad card failed the whole screen: %d %s", status, mustJSON(payload))
	}
	cards := map[string]map[string]any{}
	for _, entry := range payload["widgets"].([]any) {
		card := entry.(map[string]any)
		cards[card["id"].(string)] = card
	}

	rev := cards["a-revenue"]
	if rev["kind"] != "stat" || rev["value"] != "$1.2M" || rev["delta"] != "+12%" || rev["trend"] != "up" || rev["sentiment"] != "good" || rev["error"] != nil {
		t.Fatalf("the stat card: %v", rev)
	}
	signups := cards["b-signups"]
	if signups["kind"] != "line" || signups["span"] != float64(2) || mustJSON(signups["labels"]) != `["Mon","Tue","Wed"]` ||
		mustJSON(signups["series"]) != `[{"name":"Web","values":[1,2,3]},{"name":"Mobile","values":[0,1,5]}]` {
		t.Fatalf("the line card: %v", signups)
	}
	queue := cards["c-queue"]
	if mustJSON(queue["columns"]) != `["Queue","Depth"]` || mustJSON(queue["rows"]) != `[["mail","3"]]` {
		t.Fatalf("the table card: %v", queue)
	}
	for id, want := range map[string]string{
		"d-short":   `series "x" has 1 values for 3 labels`,
		"e-nan":     "NaN",
		"f-inf":     "+Inf",
		"g-ragged":  "row 0 has 1 cells for 2 columns",
		"h-trend":   `trend "sideways"`,
		"i-panics":  "widget panicked",
		"j-fails":   "the warehouse is down",
		"k-unnamed": "needs one for its legend",
	} {
		got, _ := cards[id]["error"].(string)
		if !strings.Contains(got, want) {
			t.Errorf("%s: the card's error is %q, want it to say %q", id, got, want)
		}
		if cards[id]["series"] != nil || cards[id]["rows"] != nil {
			t.Errorf("%s: a card that failed still carries data: %v", id, cards[id])
		}
	}
}

// TestDashboardsAnswerToTheirOwnPermission: a dashboard is listed only to
// an operator who may open it, its API answers 403 to one who may not and
// 404 for one that does not exist, a grant on the overview opens no
// dashboard, and a card's own permission is an action on its dashboard.
func TestDashboardsAnswerToTheirOwnPermission(t *testing.T) {
	line := seriesOf([]string{"Q1", "Q2"}, Series{Name: "Revenue", Values: []float64{10, 12}})
	_, provider, srv := dashboardPanel(t, []Widget{
		{ID: "open", Load: valueLoad("7")},
	}, []Dashboard{{
		ID: "finance", Title: "Finance", Columns: 2,
		Widgets: []Widget{
			{ID: "revenue", Title: "Revenue", Kind: "line", Series: line},
			{ID: "payroll", Title: "Payroll", Kind: "stat", Permission: "payroll", Stat: func(context.Context) (StatValue, error) {
				return StatValue{Value: "$80k"}, nil
			}},
		},
	}},
		[3]string{"staff", "admin:dashboard", "view"},
		[3]string{"cfo", "admin:dashboard:finance", "view"},
		[3]string{"cfo", "admin:dashboard:finance", "payroll"},
		[3]string{"analyst", "admin:dashboard:finance", "view"},
	)

	navFor := func() []string {
		payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/extensions", nil)
		if status != http.StatusOK {
			t.Fatalf("navigation status=%d", status)
		}
		var ids []string
		for _, entry := range payload["dashboards"].([]any) {
			d := entry.(map[string]any)
			ids = append(ids, d["id"].(string)+"="+d["url"].(string))
		}
		return ids
	}

	// The overview's grant opens the overview, and nothing else.
	provider.user = &auth.User{ID: "staff", Username: "staff", Role: "staff"}
	if got := navFor(); len(got) != 0 {
		t.Fatalf("staff is shown %v", got)
	}
	if _, status := rawGet(t, srv+"/api/ui/dashboards/finance"); status != http.StatusForbidden {
		t.Fatalf("staff opened the finance dashboard: %d", status)
	}
	if _, status := rawGet(t, srv+"/api/ui/dashboards/nowhere"); status != http.StatusNotFound {
		t.Fatalf("an unknown dashboard answered %d", status)
	}
	if body, _ := rawGet(t, srv+"/api/ui/dashboard"); strings.Contains(body, "Revenue") {
		t.Fatalf("a dashboard's card reached the overview: %s", body)
	}

	// The analyst opens the dashboard, and sees the cards with no
	// permission of their own.
	provider.user = &auth.User{ID: "analyst", Username: "analyst", Role: "analyst"}
	if got := navFor(); len(got) != 1 || got[0] != "finance=/admin/dashboards/finance" {
		t.Fatalf("the analyst's navigation: %v", got)
	}
	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/finance", nil)
	if status != http.StatusOK || payload["title"] != "Finance" || payload["columns"] != float64(2) {
		t.Fatalf("the analyst's dashboard (%d): %s", status, mustJSON(payload))
	}
	if raw := mustJSON(payload["widgets"]); !strings.Contains(raw, `"revenue"`) || strings.Contains(raw, "payroll") {
		t.Fatalf("the analyst's cards: %s", raw)
	}

	// The CFO holds the card's own permission too.
	provider.user = &auth.User{ID: "cfo", Username: "cfo", Role: "cfo"}
	payload, _ = doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/finance", nil)
	if raw := mustJSON(payload["widgets"]); !strings.Contains(raw, "$80k") || !strings.Contains(raw, `"revenue"`) {
		t.Fatalf("the CFO's cards: %s", raw)
	}
	// Declared order is the layout: revenue first, payroll second.
	if ws := payload["widgets"].([]any); ws[0].(map[string]any)["id"] != "revenue" {
		t.Fatalf("the dashboard reordered its cards: %s", mustJSON(ws))
	}
}

// TestRecordsCardReadsAsTheOperator: the newest rows, in the declared order,
// with only the columns this operator may read — and no card at all for an
// operator who may not list the model.
func TestRecordsCardReadsAsTheOperator(t *testing.T) {
	_, provider, srv := dashboardPanel(t, nil, []Dashboard{{
		ID: "people",
		Widgets: []Widget{{
			ID: "recent-users", Kind: "records",
			Records: RecordList{Model: "AdminUser", Fields: []string{"Name", "Email"}, OrderBy: "id desc", Limit: 2},
		}},
	}},
		[3]string{"reader", "admin:dashboard:people", "view"},
		[3]string{"reader", "admin:AdminUser", "list"},
		[3]string{"reader", "admin:AdminUser.email", "deny"},
		[3]string{"outsider", "admin:dashboard:people", "view"},
	)
	for _, name := range []string{"first", "second", "third"} {
		createAdminUser(t, srv, map[string]interface{}{"email": name + "@example.test", "name": name, "active": true})
	}

	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/people", nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	card := payload["widgets"].([]any)[0].(map[string]any)
	if card["kind"] != "records" || card["model"] != "AdminUser" ||
		mustJSON(card["rows"]) != `[["third","third@example.test"],["second","second@example.test"]]` {
		t.Fatalf("the superuser's card: %s", mustJSON(card))
	}

	provider.user = &auth.User{ID: "reader", Username: "reader", Role: "reader"}
	payload, _ = doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/people", nil)
	card = payload["widgets"].([]any)[0].(map[string]any)
	if mustJSON(card["columns"]) != `["Name"]` || mustJSON(card["rows"]) != `[["third"],["second"]]` {
		t.Fatalf("a column the reader may not read was shown: %s", mustJSON(card))
	}

	provider.user = &auth.User{ID: "outsider", Username: "outsider", Role: "outsider"}
	payload, status = doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/people", nil)
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if ws, _ := payload["widgets"].([]any); len(ws) != 0 {
		t.Fatalf("an operator who may not list the model was shown its rows: %s", mustJSON(ws))
	}
}

// TestRecordCellIsText: what a record carries is drawn as text the card can
// show, and a long one is cut.
func TestRecordCellIsText(t *testing.T) {
	for in, want := range map[any]string{
		nil:                      "",
		"plain":                  "plain",
		float64(3):               "3",
		1.5:                      "1.5",
		true:                     "true",
		strings.Repeat("x", 300): strings.Repeat("x", maxCellRunes-1) + "…",
	} {
		if got := recordCell(in); got != want {
			t.Errorf("recordCell(%v) = %q, want %q", in, got, want)
		}
	}
}
