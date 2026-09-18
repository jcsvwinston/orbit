// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The landing screen an application chooses (CUST-03).
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

// Widget is one card an application puts on the panel's overview.
type Widget struct {
	// ID identifies the card. Lowercase letters, digits and dashes.
	ID string
	// Title is the label above the value.
	Title string
	// Description is the one line under it, for what a number cannot say.
	Description string
	// Permission is the RBAC action required on admin:dashboard. Empty
	// means "view": a widget is a reading of the application, and an
	// operator who may not have that reading should not be shown it.
	Permission string
	// Link, when set, is where the card leads — a panel path
	// ("/data-studio") or a page of the application's own.
	Link string
	// Load returns the value. It is called on every load of the screen,
	// with a timeout: a widget is a glance, not a report.
	Load func(ctx context.Context) (WidgetValue, error)
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

// widgetTimeout bounds one widget's Load. The overview is a screen an
// operator opens to see whether anything is wrong; a widget querying a slow
// report must not be what makes it feel broken.
const widgetTimeout = 3 * time.Second

// validWidgetID mirrors the page id rule: it travels in a payload and in a
// URL fragment, not in prose.
func validWidgetID(id string) bool { return validPageID(id) }

// validateWidgets refuses what the panel cannot draw, at startup.
func validateWidgets(widgets []Widget) ([]Widget, error) {
	seen := make(map[string]struct{}, len(widgets))
	out := make([]Widget, 0, len(widgets))
	for i, w := range widgets {
		id := strings.ToLower(strings.TrimSpace(w.ID))
		if !validWidgetID(id) {
			return nil, fmt.Errorf("widgets[%d]: ID %q must be lowercase letters, digits or dashes", i, w.ID)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("widgets[%d]: a widget with ID %q is already declared", i, id)
		}
		if w.Load == nil {
			return nil, fmt.Errorf("widgets[%d] (%s): Load is required", i, id)
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
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ValidateWidgets is validateWidgets for the module wiring.
func ValidateWidgets(widgets []Widget) error {
	_, err := validateWidgets(widgets)
	return err
}

// dashboardResource is the RBAC resource the widgets are authorized under.
// One resource for the screen, and the per-widget Permission as the action,
// so "this role sees the finance numbers" is a policy and not a fork.
const dashboardResource = "admin:dashboard"

// authorizeWidget answers whether this operator may be shown the widget.
func (p *Panel) authorizeWidget(user *auth.User, w Widget) bool {
	if p.rbac == nil {
		return true
	}
	if user != nil && user.IsSuperuser {
		return true
	}
	for _, subject := range subjectsOf(user) {
		if p.rbac.Can(subject, dashboardResource, w.Permission) {
			return true
		}
	}
	return false
}

// widgetPayload is one card as the screen receives it.
type widgetPayload struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Link        string      `json:"link,omitempty"`
	Value       string      `json:"value,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	Items       []widgetRow `json:"items,omitempty"`
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

// handleDashboardWidgets loads the widgets this operator may see.
//
// Every widget is loaded concurrently and bounded by widgetTimeout: the
// screen is as slow as its slowest card, and one application query must not
// be able to hold the overview open. A widget that fails or times out is
// reported as itself, not as an empty screen.
func (p *Panel) handleDashboardWidgets(c *router.Context) error {
	var user *auth.User
	if p.config.Auth != nil {
		user, _ = p.authenticatedUser(c.Request)
	}
	visible := make([]Widget, 0, len(p.widgets))
	for _, w := range p.widgets {
		if p.authorizeWidget(user, w) {
			visible = append(visible, w)
		}
	}

	payloads := make([]widgetPayload, len(visible))
	var wg sync.WaitGroup
	for i, w := range visible {
		wg.Add(1)
		go func(i int, w Widget) {
			defer wg.Done()
			payload := widgetPayload{ID: w.ID, Title: w.Title, Description: w.Description, Link: w.Link}
			ctx, cancel := context.WithTimeout(c.Request.Context(), widgetTimeout)
			defer cancel()
			value, err := loadWidget(ctx, w)
			if err != nil {
				payload.Error = err.Error()
				payloads[i] = payload
				return
			}
			payload.Value, payload.Detail = value.Value, value.Detail
			for _, item := range value.Items {
				payload.Items = append(payload.Items, widgetRow{Label: item.Label, Value: item.Value, Link: item.Link})
			}
			payloads[i] = payload
		}(i, w)
	}
	wg.Wait()

	return c.JSON(http.StatusOK, map[string]any{"widgets": payloads})
}

// loadWidget calls one widget's Load and turns a panic inside it into an
// error. The function belongs to the application and runs on the panel's
// goroutine: a nil map in a widget must degrade that card, not take down the
// process that serves the whole panel.
func loadWidget(ctx context.Context, w Widget) (value WidgetValue, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("widget panicked: %v", recovered)
		}
	}()
	return w.Load(ctx)
}
