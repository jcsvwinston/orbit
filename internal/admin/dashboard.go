// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// The landing screen an application chooses (CUST-03), and the cards it is
// made of.
//
// The panel opens on a fixed screen: request rate, database health, the
// counters Orbit knows how to count. What an operator of a PRODUCT wants
// there is the product's own numbers — orders awaiting review, failed
// payments today, seats left on the plan — and the panel cannot discover any
// of them.
//
// So the application declares them, the same way it declares an action
// (ADR-010): a title, and a function that returns the value. The panel
// supplies the screen, the authorization, the timeout and the degradation.
//
// A card draws one of a closed set of kinds (A11 O5): the value A6 shipped,
// a figure with its change, a line or bar chart, a table, and the newest
// rows of one of the application's models. Each reads from a function of
// its own type, so what the function returns is what the card can draw —
// and the application writes no frontend code for any of them.

// The kinds of card the panel draws. An empty Kind is WidgetKindValue, the
// card A6 shipped.
const (
	// WidgetKindValue reads Load: a value, a detail line or a short list.
	WidgetKindValue = "value"
	// WidgetKindStat reads Stat: a headline figure and its change.
	WidgetKindStat = "stat"
	// WidgetKindLine reads Series and draws it as a line chart.
	WidgetKindLine = "line"
	// WidgetKindBar reads Series and draws it as a bar chart.
	WidgetKindBar = "bar"
	// WidgetKindTable reads Table: columns and rows of text.
	WidgetKindTable = "table"
	// WidgetKindRecords reads Records: the newest rows of one model, which
	// the panel lists itself, as the operator who is looking.
	WidgetKindRecords = "records"
)

// widgetKinds is the closed set, in the order an error lists it.
var widgetKinds = []string{WidgetKindValue, WidgetKindStat, WidgetKindLine, WidgetKindBar, WidgetKindTable, WidgetKindRecords}

// Widget is one card an application puts on the panel's overview, or on one
// of its dashboards.
type Widget struct {
	// ID identifies the card. Lowercase letters, digits and dashes.
	ID string
	// Title is the label above the value.
	Title string
	// Description is the one line under it, for what a number cannot say.
	Description string
	// Permission is the RBAC action required on admin:dashboard (or, on a
	// named dashboard, on admin:dashboard:<id>). Empty means "view": a
	// widget is a reading of the application, and an operator who may not
	// have that reading should not be shown it.
	Permission string
	// Link, when set, is where the card leads — a panel path
	// ("/data-studio") or a page of the application's own.
	Link string
	// Load returns the value. It is called on every load of the screen,
	// with a timeout: a widget is a glance, not a report.
	Load func(ctx context.Context) (WidgetValue, error)

	// Kind is what the card draws: "value" (the default, read from Load),
	// "stat" (Stat), "line" or "bar" (Series), "table" (Table) or
	// "records" (Records). An unknown kind, a kind without its function, or
	// a function the kind would never call stops the application at
	// startup, naming the widget.
	Kind string
	// Stat returns a headline figure and its change, for kind "stat".
	Stat func(ctx context.Context) (StatValue, error)
	// Series returns one or more series over the same labels, for kinds
	// "line" and "bar".
	Series func(ctx context.Context) (SeriesValue, error)
	// Table returns columns and rows of text, for kind "table".
	Table func(ctx context.Context) (TableValue, error)
	// Records names the model a "records" card lists. It is the one kind
	// with no function: the panel reads the rows itself, as the operator
	// who is looking, through the same list the grid uses — so the card
	// never shows a row that operator's tenant, row or field policies hide.
	Records RecordList
	// Span is how many columns of the grid the card takes, from 1 to the
	// dashboard's Columns (4 on the overview). Zero is 1 for a value or a
	// stat and 2 for a chart, a table or a list of records — never more
	// than its screen has.
	Span int
}

// WidgetValue is what one widget shows.
type WidgetValue struct {
	// Value is the headline: a count, an amount, a state. It is a string
	// because the application knows how to format its own numbers, and a
	// panel that formatted them would have to be told the currency, the
	// locale and the precision to get it wrong in three ways.
	Value string
	// Detail is the smaller line under the value ("12% more than last
	// week", "oldest: 3 days").
	Detail string
	// Items turn the card into a short list instead of a single number.
	Items []WidgetItem
}

// WidgetItem is one row of a list widget.
type WidgetItem struct {
	Label string
	Value string
	Link  string
}

// StatValue is what a "stat" card shows: a figure, and how it moved.
type StatValue struct {
	// Value is the headline, formatted by the application.
	Value string
	// Detail is the smaller line under it.
	Detail string
	// Delta is the change, as the application formats it ("+12%",
	// "-3 since yesterday"). Empty draws no change.
	Delta string
	// Trend is which way the change goes: "up", "down" or "flat". It is
	// what the arrow draws and what a screen reader says. Empty draws the
	// delta with no arrow.
	Trend string
	// Sentiment says whether the change is good news: "good", "bad", or
	// empty for neither. Up is not always good — failed payments going up
	// is the reason a card exists — so the direction does not decide it.
	Sentiment string
}

// SeriesValue is what a "line" or "bar" card draws: one value per label for
// each series.
type SeriesValue struct {
	// Labels are the points of the horizontal axis, in order — days, hours,
	// categories — formatted by the application.
	Labels []string
	// Series are the lines, or the bars of each label. Every one carries
	// exactly one value per label.
	Series []Series
	// Detail is a line under the chart.
	Detail string
}

// Series is one named sequence of a chart.
type Series struct {
	// Name is the legend's label. It may be empty when the chart has one
	// series and its title says what it counts.
	Name string
	// Values are the readings, one per label of the SeriesValue. A value
	// that is not a finite number cannot be drawn.
	Values []float64
}

// TableValue is what a "table" card shows: columns and rows of text the
// application formatted.
type TableValue struct {
	Columns []string
	Rows    [][]string
	// Detail is a line under the table.
	Detail string
}

// RecordList is what a "records" card lists: the newest rows of one of the
// application's models.
type RecordList struct {
	// Model is the model whose rows are listed. Required.
	Model string
	// Fields are the columns shown, by Go name or column. Empty is the
	// model's list fields, or else its first three.
	Fields []string
	// OrderBy is the order, in the grid's syntax: "created_at desc". Empty
	// is newest first — by created_at when the model has one, else by its
	// primary key.
	OrderBy string
	// Limit is how many rows: zero is 5, and at most 50.
	Limit int
}

// The bounds of what one card may carry. A widget is a glance: a payload that
// passes them is a report, and the panel says so on the card instead of
// shipping it.
const (
	maxSeries        = 8
	maxSeriesPoints  = 500
	maxTableColumns  = 12
	maxTableRows     = 100
	maxRecordsLimit  = 50
	defaultRecords   = 5
	maxGridColumns   = 4
	defaultChartSpan = 2
)

// widgetTimeout bounds one widget's Load. The overview is a screen an
// operator opens to see whether anything is wrong; a widget querying a slow
// report must not be what makes it feel broken.
const widgetTimeout = 3 * time.Second

// validWidgetID mirrors the page id rule: it travels in a payload and in a
// URL fragment, not in prose.
func validWidgetID(id string) bool { return validPageID(id) }

// modelLookup finds one of the application's models, for the "records"
// cards. A nil lookup finds none.
type modelLookup func(name string) (datasource.ModelInfo, bool)

// lookupIn adapts a data source to a modelLookup.
func lookupIn(src datasource.DataSource) modelLookup {
	if src == nil {
		return nil
	}
	return src.Get
}

// validateWidgets refuses what the panel cannot draw on the overview, at
// startup. The overview orders its cards by ID, as it has since A6.
func validateWidgets(widgets []Widget) ([]Widget, error) {
	return validateWidgetList("widgets", widgets, nil, maxGridColumns, true)
}

// validateWidgetList checks one screen's cards: where names the list in the
// error ("widgets", "dashboards[0] (finance).widgets"), models resolves the
// "records" cards, columns is the widest span the screen allows, and byID
// keeps the overview's order.
func validateWidgetList(where string, widgets []Widget, models modelLookup, columns int, byID bool) ([]Widget, error) {
	seen := make(map[string]struct{}, len(widgets))
	out := make([]Widget, 0, len(widgets))
	for i, w := range widgets {
		id := strings.ToLower(strings.TrimSpace(w.ID))
		if !validWidgetID(id) {
			return nil, fmt.Errorf("%s[%d]: ID %q must be lowercase letters, digits or dashes", where, i, w.ID)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s[%d]: a widget with ID %q is already declared", where, i, id)
		}
		label := fmt.Sprintf("%s[%d] (%s)", where, i, id)
		compiled, err := compileWidgetKind(label, w, models)
		if err != nil {
			return nil, err
		}
		w = compiled
		if w.Span < 0 || w.Span > columns {
			return nil, fmt.Errorf("%s: Span %d is outside 1..%d, the columns of its screen", label, w.Span, columns)
		}
		if w.Span == 0 {
			w.Span = defaultSpan(w.Kind, columns)
		}
		seen[id] = struct{}{}
		w.ID = id
		if strings.TrimSpace(w.Title) == "" {
			w.Title = id
		}
		if strings.TrimSpace(w.Permission) == "" {
			w.Permission = "view"
		}
		out = append(out, w)
	}
	if byID {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	}
	return out, nil
}

// compileWidgetKind checks that the widget's kind is one the panel draws,
// that the function the kind reads is there, and that no other function is:
// a Series declared on a "stat" card would never be called, which is a card
// that silently draws something other than what its author wrote.
func compileWidgetKind(label string, w Widget, models modelLookup) (Widget, error) {
	kind := strings.ToLower(strings.TrimSpace(w.Kind))
	if kind == WidgetKindValue {
		kind = ""
	}
	declared := map[string]bool{
		"Load":    w.Load != nil,
		"Stat":    w.Stat != nil,
		"Series":  w.Series != nil,
		"Table":   w.Table != nil,
		"Records": strings.TrimSpace(w.Records.Model) != "" || len(w.Records.Fields) > 0 || w.Records.OrderBy != "" || w.Records.Limit != 0,
	}
	var reads string
	switch kind {
	case "":
		reads = "Load"
	case WidgetKindStat:
		reads = "Stat"
	case WidgetKindLine, WidgetKindBar:
		reads = "Series"
	case WidgetKindTable:
		reads = "Table"
	case WidgetKindRecords:
		reads = "Records"
	default:
		return w, fmt.Errorf("%s: Kind %q is not one the panel draws (%s)", label, w.Kind, strings.Join(widgetKinds, ", "))
	}
	if !declared[reads] {
		if kind == "" {
			return w, fmt.Errorf("%s: Load is required", label)
		}
		if kind == WidgetKindRecords {
			return w, fmt.Errorf("%s: kind %q needs Records.Model", label, kind)
		}
		return w, fmt.Errorf("%s: kind %q needs %s", label, kind, reads)
	}
	for _, other := range []string{"Load", "Stat", "Series", "Table", "Records"} {
		if other != reads && declared[other] {
			shown := kind
			if shown == "" {
				shown = WidgetKindValue
			}
			return w, fmt.Errorf("%s: %s is set, and a %q card never reads it (it reads %s)", label, other, shown, reads)
		}
	}
	w.Kind = kind
	if kind == WidgetKindRecords {
		list, err := compileRecordList(label, w.Records, models)
		if err != nil {
			return w, err
		}
		w.Records = list
	}
	return w, nil
}

// compileRecordList resolves a "records" card against the application's
// models: the model exists, every field is one the grid would show, and the
// order is one the grid would accept.
func compileRecordList(label string, list RecordList, models modelLookup) (RecordList, error) {
	var mi datasource.ModelInfo
	found := false
	if models != nil {
		mi, found = models(strings.TrimSpace(list.Model))
	}
	if !found {
		return list, fmt.Errorf("%s: Records.Model: no model named %q in this application", label, list.Model)
	}
	list.Model = mi.Name
	if list.Limit < 0 || list.Limit > maxRecordsLimit {
		return list, fmt.Errorf("%s: Records.Limit %d is outside 1..%d", label, list.Limit, maxRecordsLimit)
	}
	if list.Limit == 0 {
		list.Limit = defaultRecords
	}
	fields := make([]string, 0, len(list.Fields))
	for _, key := range list.Fields {
		_, f, ok := dsResolveField(mi, key)
		if !ok {
			return list, fmt.Errorf("%s: Records.Fields: %s has no field %q", label, mi.Name, key)
		}
		if f.IsExcluded {
			return list, fmt.Errorf("%s: Records.Fields: %s.%s is excluded from the panel", label, mi.Name, f.Name)
		}
		fields = append(fields, f.Name)
	}
	list.Fields = fields
	order := strings.TrimSpace(list.OrderBy)
	if order == "" {
		order = defaultRecordOrder(mi)
	}
	sanitized, err := dsSanitizeOrderBy(mi, order)
	if err != nil || sanitized == "" {
		return list, fmt.Errorf("%s: Records.OrderBy %q is not an order the grid accepts for %s (a field, then asc or desc)", label, list.OrderBy, mi.Name)
	}
	list.OrderBy = sanitized
	return list, nil
}

// defaultRecordOrder is "newest first": by created_at when the model has it,
// else by its primary key.
func defaultRecordOrder(mi datasource.ModelInfo) string {
	if _, f, ok := dsResolveField(mi, "created_at"); ok && !f.IsExcluded {
		return "created_at desc"
	}
	return "id desc"
}

// dashboardResource is the RBAC resource the overview's widgets are
// authorized under. One resource for the screen, and the per-widget
// Permission as the action, so "this role sees the finance numbers" is a
// policy and not a fork. A named dashboard is its own resource
// (dashboardResourceFor).
const dashboardResource = "admin:dashboard"

// authorizeWidget answers whether this operator may be shown the widget on
// the overview.
func (p *Panel) authorizeWidget(user *auth.User, w Widget) bool {
	return p.authorizeWidgetOn(user, dashboardResource, w)
}

// authorizeWidgetOn answers it for the screen whose resource is given.
func (p *Panel) authorizeWidgetOn(user *auth.User, resource string, w Widget) bool {
	if p.rbac == nil {
		return true
	}
	if user != nil && user.IsSuperuser {
		return true
	}
	for _, subject := range subjectsOf(user) {
		if p.rbac.Can(subject, resource, w.Permission) {
			return true
		}
	}
	return false
}

// widgetPayload is one card as the screen receives it. The members a value
// card carries are the ones it carried in A6, in the same order; everything
// a newer kind adds is omitted when empty, so an overview declared before
// the kinds existed is served byte for byte as it was.
type widgetPayload struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind,omitempty"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Link        string      `json:"link,omitempty"`
	Span        int         `json:"span,omitempty"`
	Value       string      `json:"value,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	Items       []widgetRow `json:"items,omitempty"`
	// A stat's change.
	Delta     string `json:"delta,omitempty"`
	Trend     string `json:"trend,omitempty"`
	Sentiment string `json:"sentiment,omitempty"`
	// A chart's axis and series.
	Labels []string        `json:"labels,omitempty"`
	Series []seriesPayload `json:"series,omitempty"`
	// A table's, or a list of records', columns and rows.
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	Model   string     `json:"model,omitempty"`
	// Error carries a widget's own failure. The card is still drawn, saying
	// it could not be read: a screen that drops the card would report a
	// broken query as "nothing to see".
	Error string `json:"error,omitempty"`
}

type widgetRow struct {
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
	Link  string `json:"link,omitempty"`
}

type seriesPayload struct {
	Name   string    `json:"name,omitempty"`
	Values []float64 `json:"values"`
}

// handleDashboardWidgets loads the overview's widgets this operator may see.
func (p *Panel) handleDashboardWidgets(c *router.Context) error {
	var user *auth.User
	if p.config.Auth != nil {
		user, _ = p.authenticatedUser(c.Request)
	}
	return c.JSON(http.StatusOK, map[string]any{"widgets": p.loadWidgets(c, user, dashboardResource, p.widgets)})
}

// loadWidgets loads the cards of one screen that this operator may see.
//
// Every widget is loaded concurrently and bounded by widgetTimeout: the
// screen is as slow as its slowest card, and one application query must not
// be able to hold it open. A widget that fails, panics, times out or returns
// something its card cannot draw is reported as itself, not as an empty or a
// failed screen.
func (p *Panel) loadWidgets(c *router.Context, user *auth.User, resource string, widgets []Widget) []widgetPayload {
	type visibleWidget struct {
		widget  Widget
		records *recordsRead
	}
	visible := make([]visibleWidget, 0, len(widgets))
	for _, w := range widgets {
		if !p.authorizeWidgetOn(user, resource, w) {
			continue
		}
		entry := visibleWidget{widget: w}
		if w.Kind == WidgetKindRecords {
			// A list of a model's rows is the model's to show: an operator
			// who may not list it is not shown the card at all, exactly as
			// the grid would not show them the rows.
			read, ok := p.prepareRecordsRead(c, w.Records)
			if !ok {
				continue
			}
			entry.records = read
		}
		visible = append(visible, entry)
	}

	payloads := make([]widgetPayload, len(visible))
	var wg sync.WaitGroup
	for i, entry := range visible {
		wg.Add(1)
		go func(i int, entry visibleWidget) {
			defer wg.Done()
			w := entry.widget
			payload := widgetPayload{ID: w.ID, Kind: w.Kind, Title: w.Title, Description: w.Description, Link: w.Link, Span: w.Span}
			ctx, cancel := context.WithTimeout(c.Request.Context(), widgetTimeout)
			defer cancel()
			if err := p.fillWidget(ctx, w, entry.records, &payload); err != nil {
				payload.Error = err.Error()
			}
			payloads[i] = payload
		}(i, entry)
	}
	wg.Wait()
	return payloads
}

// defaultSpan is the columns a card declared with no Span takes: two for a
// chart, a table or a list of records — never more than its screen has —
// and, for a value or a stat, none in the payload, which is how a value card
// was served in A6 and what the screen draws as one column.
func defaultSpan(kind string, columns int) int {
	switch kind {
	case WidgetKindLine, WidgetKindBar, WidgetKindTable, WidgetKindRecords:
		return min(defaultChartSpan, columns)
	}
	return 0
}

// fillWidget reads one widget into its payload, turning a panic inside the
// application's function into an error. The function belongs to the
// application and runs on the panel's goroutine: a nil map in a widget must
// degrade that card, not take down the process that serves the whole panel.
func (p *Panel) fillWidget(ctx context.Context, w Widget, records *recordsRead, payload *widgetPayload) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("widget panicked: %v", recovered)
		}
	}()
	switch w.Kind {
	case WidgetKindStat:
		value, err := w.Stat(ctx)
		if err != nil {
			return err
		}
		return fillStat(value, payload)
	case WidgetKindLine, WidgetKindBar:
		value, err := w.Series(ctx)
		if err != nil {
			return err
		}
		return fillSeries(value, payload)
	case WidgetKindTable:
		value, err := w.Table(ctx)
		if err != nil {
			return err
		}
		return fillTable(value, payload)
	case WidgetKindRecords:
		return p.fillRecords(ctx, records, payload)
	}
	value, err := w.Load(ctx)
	if err != nil {
		return err
	}
	payload.Value, payload.Detail = value.Value, value.Detail
	for _, item := range value.Items {
		payload.Items = append(payload.Items, widgetRow{Label: item.Label, Value: item.Value, Link: item.Link})
	}
	return nil
}

// fillStat checks what a "stat" function returned. A trend or a sentiment
// outside the words the card knows is a card that would draw the wrong
// arrow, or none, so it is the card's error.
func fillStat(v StatValue, payload *widgetPayload) error {
	trend := strings.ToLower(strings.TrimSpace(v.Trend))
	switch trend {
	case "", "up", "down", "flat":
	default:
		return fmt.Errorf("trend %q is not up, down or flat", v.Trend)
	}
	sentiment := strings.ToLower(strings.TrimSpace(v.Sentiment))
	switch sentiment {
	case "", "good", "bad":
	default:
		return fmt.Errorf("sentiment %q is not good or bad", v.Sentiment)
	}
	if trend != "" && strings.TrimSpace(v.Delta) == "" {
		return fmt.Errorf("trend %q has no delta to draw it on", trend)
	}
	payload.Value, payload.Detail = v.Value, v.Detail
	payload.Delta, payload.Trend, payload.Sentiment = v.Delta, trend, sentiment
	return nil
}

// fillSeries checks what a "line" or "bar" function returned. Every check is
// one the chart would otherwise fail in a way nobody could read: a series
// shorter than its axis draws points against the wrong labels, two series of
// one name are one legend entry for two lines, and a value that is not a
// finite number cannot be written as JSON at all — it would have failed the
// whole screen instead of this card.
func fillSeries(v SeriesValue, payload *widgetPayload) error {
	if len(v.Labels) > maxSeriesPoints {
		return fmt.Errorf("the chart has %d points; a card draws at most %d", len(v.Labels), maxSeriesPoints)
	}
	if len(v.Series) > maxSeries {
		return fmt.Errorf("the chart has %d series; a card draws at most %d", len(v.Series), maxSeries)
	}
	if len(v.Labels) > 0 && len(v.Series) == 0 {
		return fmt.Errorf("the chart has %d labels and no series", len(v.Labels))
	}
	names := make(map[string]bool, len(v.Series))
	series := make([]seriesPayload, 0, len(v.Series))
	for i, s := range v.Series {
		name := strings.TrimSpace(s.Name)
		if name == "" && len(v.Series) > 1 {
			return fmt.Errorf("series %d has no name, and a chart of %d series needs one for its legend", i, len(v.Series))
		}
		if names[name] {
			return fmt.Errorf("two series are named %q", name)
		}
		names[name] = true
		if len(s.Values) != len(v.Labels) {
			return fmt.Errorf("series %q has %d values for %d labels", name, len(s.Values), len(v.Labels))
		}
		for j, value := range s.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("series %q at %q is %v, which a chart cannot draw", name, v.Labels[j], value)
			}
		}
		series = append(series, seriesPayload{Name: name, Values: append([]float64{}, s.Values...)})
	}
	payload.Labels = append([]string{}, v.Labels...)
	payload.Series = series
	payload.Detail = v.Detail
	return nil
}

// fillTable checks what a "table" function returned.
func fillTable(v TableValue, payload *widgetPayload) error {
	if len(v.Columns) > maxTableColumns {
		return fmt.Errorf("the table has %d columns; a card draws at most %d", len(v.Columns), maxTableColumns)
	}
	if len(v.Rows) > maxTableRows {
		return fmt.Errorf("the table has %d rows; a card draws at most %d — page it in the application", len(v.Rows), maxTableRows)
	}
	if len(v.Rows) > 0 && len(v.Columns) == 0 {
		return fmt.Errorf("the table has %d rows and no columns", len(v.Rows))
	}
	rows := make([][]string, 0, len(v.Rows))
	for i, row := range v.Rows {
		if len(row) != len(v.Columns) {
			return fmt.Errorf("row %d has %d cells for %d columns", i, len(row), len(v.Columns))
		}
		rows = append(rows, append([]string{}, row...))
	}
	payload.Columns = append([]string{}, v.Columns...)
	payload.Rows = rows
	payload.Detail = v.Detail
	return nil
}
