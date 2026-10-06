// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Screens of cards beyond the overview (A11 O5).
//
// The overview is the one screen every operator opens. A product grows
// readings that belong to some operators and not others — the finance
// numbers, the support queue, the fleet's error rates — and putting all of
// them on the landing screen either crowds it or hides them behind the
// per-card permission one at a time. A dashboard is a screen of its own for
// those: named, listed in the navigation, made of the same cards, and gated
// as a whole by its own permission.
//
// The overview is not one of them and does not change: Config.Widgets are
// still its cards, still on admin:dashboard, still ordered by ID.

// Dashboard is a screen of cards an application adds beside the overview.
type Dashboard struct {
	// ID is the path segment it is served under (<prefix>/dashboards/<id>)
	// and the end of its RBAC resource (admin:dashboard:<id>). Lowercase
	// letters, digits and dashes.
	ID string
	// Title is what the navigation entry and the screen's heading read.
	Title string
	// Description is the one line under the heading.
	Description string
	// Permission is the RBAC action required on admin:dashboard:<id>. Empty
	// means "view". An operator without it does not see the dashboard in
	// the navigation, and its API answers them 403. A superuser always
	// passes, as everywhere else in the panel.
	Permission string
	// Columns is how many columns its grid has on a wide screen, from 1 to
	// 4. Zero is 4, the overview's.
	Columns int
	// Widgets are its cards, drawn in the order they are declared — the
	// order and each card's Span are the layout. Each card's own
	// Permission is an action on admin:dashboard:<id>. A dashboard with no
	// cards is a blank screen, and refuses to start.
	Widgets []Widget
}

// dashboardResourceFor is the RBAC resource of one named dashboard. It is a
// separate resource from the overview's (admin:dashboard), so a grant that
// opens one never opens another; a model's name cannot hold a colon, so it
// can never be a model's resource either.
func dashboardResourceFor(id string) string { return dashboardResource + ":" + id }

// dashboardPath is where the screen lives in the panel, relative to its
// prefix; the API that feeds it is /api/ui/dashboards/<id>.
func dashboardPath(id string) string { return "/dashboards/" + id }

// validateDashboards checks the declared dashboards before the panel serves
// them, for the same reason the pages are checked: each of these mistakes is
// otherwise a screen that silently never appears, or appears empty.
func validateDashboards(dashboards []Dashboard, models modelLookup) ([]Dashboard, error) {
	seen := make(map[string]struct{}, len(dashboards))
	out := make([]Dashboard, 0, len(dashboards))
	for i, d := range dashboards {
		id := strings.ToLower(strings.TrimSpace(d.ID))
		if !validPageID(id) {
			return nil, fmt.Errorf("dashboards[%d]: ID %q must be lowercase letters, digits or dashes", i, d.ID)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("dashboards[%d]: a dashboard with ID %q is already declared", i, id)
		}
		label := fmt.Sprintf("dashboards[%d] (%s)", i, id)
		if d.Columns < 0 || d.Columns > maxGridColumns {
			return nil, fmt.Errorf("%s: Columns %d is outside 1..%d", label, d.Columns, maxGridColumns)
		}
		if d.Columns == 0 {
			d.Columns = maxGridColumns
		}
		if len(d.Widgets) == 0 {
			return nil, fmt.Errorf("%s: a dashboard needs at least one widget", label)
		}
		widgets, err := validateWidgetList(label+".widgets", d.Widgets, models, d.Columns, false)
		if err != nil {
			return nil, err
		}
		seen[id] = struct{}{}
		d.ID = id
		d.Widgets = widgets
		if strings.TrimSpace(d.Title) == "" {
			d.Title = id
		}
		if strings.TrimSpace(d.Permission) == "" {
			d.Permission = "view"
		}
		out = append(out, d)
	}
	return out, nil
}

// ValidateDashboards checks the overview's widgets and the named dashboards
// against this application's models, for the module wiring, which reports
// the error as a refusal to start.
func ValidateDashboards(src datasource.DataSource, widgets []Widget, dashboards []Dashboard) error {
	models := lookupIn(src)
	if _, err := validateWidgetList("widgets", widgets, models, maxGridColumns, true); err != nil {
		return err
	}
	_, err := validateDashboards(dashboards, models)
	return err
}

// dashboardByID finds a declared dashboard.
func (p *Panel) dashboardByID(id string) (Dashboard, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, d := range p.dashboards {
		if d.ID == id {
			return d, true
		}
	}
	return Dashboard{}, false
}

// authorizeDashboard answers whether this operator may open the dashboard.
func (p *Panel) authorizeDashboard(user *auth.User, d Dashboard) error {
	if p.rbac == nil {
		// No enforcer: the panel's auth provider is the only opinion there
		// is, and it answers about models — the posture a page has too.
		return nil
	}
	if user != nil && user.IsSuperuser {
		return nil
	}
	resource := dashboardResourceFor(d.ID)
	for _, subject := range subjectsOf(user) {
		if p.rbac.Can(subject, resource, d.Permission) {
			return nil
		}
	}
	return authDeniedDomain("dashboard:"+d.ID, d.Permission)
}

// dashboardDescriptor is one dashboard as the navigation payload carries it.
type dashboardDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// URL is absolute from the site root, like a page's.
	URL string `json:"url"`
}

// visibleDashboards lists the dashboards this operator may open, for the
// navigation. One they may not open is absent, not greyed out: the list IS
// the navigation, and a link that refuses is a worse answer than no link.
func (p *Panel) visibleDashboards(user *auth.User) []dashboardDescriptor {
	prefix := strings.TrimSuffix(NormalizePrefix(p.config.Prefix), "/")
	out := make([]dashboardDescriptor, 0, len(p.dashboards))
	for _, d := range p.dashboards {
		if err := p.authorizeDashboard(user, d); err != nil {
			continue
		}
		out = append(out, dashboardDescriptor{
			ID: d.ID, Title: d.Title, Description: d.Description,
			URL: prefix + dashboardPath(d.ID),
		})
	}
	return out
}

// handleNamedDashboard serves one dashboard's cards. An unknown id is a 404
// for everybody; a known one this operator may not open is a 403, the same
// answer a page gives.
func (p *Panel) handleNamedDashboard(c *router.Context) error {
	d, ok := p.dashboardByID(c.Param("id"))
	if !ok {
		return gferrors.NotFound("dashboard", c.Param("id"))
	}
	var user *auth.User
	if p.config.Auth != nil {
		var err error
		user, err = p.authenticatedUser(c.Request)
		if err != nil {
			return p.authErrorToDomain(err)
		}
	}
	if err := p.authorizeDashboard(user, d); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id":          d.ID,
		"title":       d.Title,
		"description": d.Description,
		"columns":     d.Columns,
		"widgets":     p.loadWidgets(c, user, dashboardResourceFor(d.ID), d.Widgets),
	})
}

// recordsRead is everything a "records" card needs from the request, taken
// before its goroutine starts: which model, which store, and the confinement
// this operator's grid would apply.
type recordsRead struct {
	model   datasource.ModelInfo
	list    RecordList
	filters map[string]string
	rules   fieldRules
}

// prepareRecordsRead authorizes a "records" card and fixes its confinement.
// It answers false when the operator may not list the model — the card is
// then not shown — and also when the model has gone from the registry since
// the panel started.
//
// It also answers false when the card's order names a field this operator
// does not read (OR-69): the rows in that order are the ranking the field
// holds — "the five largest salaries", without the salaries — which is the
// sort the grid refuses them.
func (p *Panel) prepareRecordsRead(c *router.Context, list RecordList) (*recordsRead, bool) {
	mi, ok := p.src.Get(list.Model)
	if !ok {
		return nil, false
	}
	read, err := p.requestReadScope(c, mi, "list")
	if err != nil {
		return nil, false
	}
	if _, err := dsSanitizeOrderBy(read.fields.queryModel(mi), list.OrderBy); err != nil {
		return nil, false
	}
	return &recordsRead{model: mi, list: list, filters: read.filters(nil), rules: read.fields}, true
}

// fillRecords lists the newest rows of the card's model, as the operator who
// is looking: the tenant and row confinement prepareRecordsRead fixed, and
// only the columns their field permissions let them read.
func (p *Panel) fillRecords(ctx context.Context, read *recordsRead, payload *widgetPayload) error {
	if read == nil {
		return fmt.Errorf("the records card was not prepared")
	}
	mi := read.model
	st, err := p.src.Store(mi.Name, mi.DatabaseAlias)
	if err != nil {
		return err
	}
	page, err := st.List(ctx, datasource.Query{
		Page:     1,
		PageSize: read.list.Limit,
		Filters:  read.filters,
		OrderBy:  read.list.OrderBy,
	})
	if err != nil {
		return err
	}
	read.rules.maskAll(mi, page.Items)

	fields := recordListFields(mi, read.list.Fields)
	columns := make([]string, 0, len(fields))
	shown := make([]datasource.FieldInfo, 0, len(fields))
	for _, f := range fields {
		if !read.rules.readable(runtimeColumn(f.Column)) {
			continue
		}
		label := strings.TrimSpace(f.Label)
		if label == "" {
			label = f.Name
		}
		columns = append(columns, label)
		shown = append(shown, f)
	}
	rows := make([][]string, 0, len(page.Items))
	for _, rec := range page.Items {
		row := make([]string, 0, len(shown))
		for _, f := range shown {
			value, _ := recordValueByKeys(rec, fieldKeys(f, p.fieldJSONKey(mi.Name, f)))
			row = append(row, recordCell(value))
		}
		rows = append(rows, row)
	}
	payload.Model = mi.Name
	payload.Columns = columns
	payload.Rows = rows
	return nil
}

// recordListFields resolves the columns a "records" card shows: the ones
// declared, else the model's list fields, else its first three — never the
// primary key on its own, and never a field excluded from the panel.
func recordListFields(mi datasource.ModelInfo, declared []string) []datasource.FieldInfo {
	var out []datasource.FieldInfo
	if len(declared) > 0 {
		for _, key := range declared {
			if _, f, ok := dsResolveField(mi, key); ok && !f.IsExcluded {
				out = append(out, f)
			}
		}
		return out
	}
	for _, f := range mi.Fields {
		if f.IsList && !f.IsPK && !f.IsExcluded {
			out = append(out, f)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, f := range mi.Fields {
		if f.IsPK || f.IsExcluded {
			continue
		}
		out = append(out, f)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// maxCellRunes is where a cell of a "records" card is cut: it is a glance
// at a row, and the record itself is one click away in Data Studio.
const maxCellRunes = 120

// recordCell renders one value of a record as the card's text.
func recordCell(v any) string {
	var s string
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		s = value
	case []byte:
		s = string(value)
	case time.Time:
		if value.IsZero() {
			return ""
		}
		s = value.UTC().Format("2006-01-02 15:04")
	case *time.Time:
		if value == nil || value.IsZero() {
			return ""
		}
		s = value.UTC().Format("2006-01-02 15:04")
	case float64:
		s = strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(value)
	case fmt.Stringer:
		s = value.String()
	case map[string]any, []any:
		raw, err := json.Marshal(value)
		if err != nil {
			s = fmt.Sprint(value)
		} else {
			s = string(raw)
		}
	default:
		s = fmt.Sprint(value)
	}
	if runes := []rune(s); len(runes) > maxCellRunes {
		s = string(runes[:maxCellRunes-1]) + "…"
	}
	return s
}
