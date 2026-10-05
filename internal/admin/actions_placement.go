// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Where an action is offered (EXT-02).
//
// Until actions could say where they belong, every one was a verb over the
// grid's selection, and the only way to run one on the record an operator
// was looking at was to go back to the grid, find the row and tick it.
// "Refund this order" and "open the reconciliation of this batch" are about
// one record, and the record view is where an operator decides to do them.
//
// The placement is a declaration the server holds the action to, not only a
// hint about where to draw a button: an action written for one record
// receives exactly one id, and one written for a selection is not reachable
// from a record it was never offered on.

// ActionPlacement says where the panel offers an action.
type ActionPlacement string

// The places an action can be offered.
const (
	// ActionOnSelection offers the action over the grid's selection. It is
	// what the zero value means, so an action declared before placements
	// existed is offered exactly where it was.
	ActionOnSelection ActionPlacement = "selection"
	// ActionOnRecord offers the action on one record: in its record view
	// and in its row's menu. The action receives exactly that record's id.
	ActionOnRecord ActionPlacement = "record"
	// ActionOnSelectionAndRecord offers the action in both places.
	ActionOnSelectionAndRecord ActionPlacement = "selection_and_record"
)

// offers reports whether an action with this placement may be called from
// where: ActionOnSelection (the bulk endpoint) or ActionOnRecord (the record
// endpoint).
func (pl ActionPlacement) offers(where ActionPlacement) bool {
	switch pl {
	case ActionOnSelectionAndRecord:
		return where == ActionOnSelection || where == ActionOnRecord
	case "", ActionOnSelection:
		return where == ActionOnSelection
	default:
		return pl == where
	}
}

// validateActionPlacement normalises an action's placement and refuses the
// ones the panel cannot honour. where names the action the way
// validateModelActions does.
func validateActionPlacement(where string, action ModelAction) (ActionPlacement, error) {
	placement := ActionPlacement(strings.ToLower(strings.TrimSpace(string(action.Placement))))
	switch placement {
	case "":
		placement = ActionOnSelection
	case ActionOnSelection, ActionOnRecord, ActionOnSelectionAndRecord:
	default:
		return "", fmt.Errorf("%s: unknown placement %q (the panel offers an action on %q, %q or %q)",
			where, action.Placement, ActionOnSelection, ActionOnRecord, ActionOnSelectionAndRecord)
	}
	// A record action always has its record; "it may run with none" would
	// be a promise about a button that cannot exist.
	if placement == ActionOnRecord && action.AllowEmptySelection {
		return "", fmt.Errorf("%s: AllowEmptySelection has no meaning for an action offered only on a record, which always runs on one", where)
	}
	return placement, nil
}

// actionPlacementRefusal is the 400 for an action called from a place it is
// not offered, and it says where it IS offered, so the mistake is fixed
// where it was made rather than guessed at.
func actionPlacementRefusal(mi datasource.ModelInfo, action ModelAction, from ActionPlacement) error {
	if from == ActionOnRecord {
		return gferrors.BadRequest(fmt.Sprintf(
			"the %s action is offered on a selection, not on one record: POST /api/models/%s/bulk with the record's id",
			action.Name, mi.Name))
	}
	return gferrors.BadRequest(fmt.Sprintf(
		"the %s action is offered on one record, not on a selection: POST /api/models/%s/actions/%s/{id}",
		action.Name, mi.Name, action.Name))
}

// handleRecordAction runs a declared action on one record:
// POST /api/models/{name}/actions/{action}/{id}, with {"input": {...}} when
// the action declares fields. It is the record view's entry point, and it
// runs through the same code as the bulk endpoint — authorization by the
// verb, tenant and row confinement, the input check, the audit entry and
// the answer — over exactly one id.
func (p *Panel) handleRecordAction(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	verb := strings.ToLower(strings.TrimSpace(c.Param("action")))
	action, ok := p.modelActions[actionKey{model: mi.Name, name: verb}]
	if !ok {
		return gferrors.BadRequest("unknown action: " + verb)
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return gferrors.BadRequest("a record action needs the record's id")
	}

	// The body is optional: an action that asks for nothing is posted
	// with none, and one that asks reads its answers from "input".
	var req struct {
		Input json.RawMessage `json:"input"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return gferrors.BadRequest("invalid JSON")
		}
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// The model's declared database, unless the request names one — the
	// same fallback the record and bulk endpoints apply.
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	return p.performModelAction(c, mi, actionCall{
		action: action, from: ActionOnRecord,
		ids: []string{id}, rawInput: req.Input, database: databaseAlias,
	})
}
