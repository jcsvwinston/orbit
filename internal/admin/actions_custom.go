// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Actions an application defines for its own models (DS-09).
//
// The panel's own verbs are the ones every table has — create, update,
// delete, export. What a product needs on top of them is its own: publish
// these three drafts, re-send this invoice, retry these payments. Until this
// contract the bulk endpoint's verb list was closed, so the only way to put
// such a button on a grid was to fork the panel.
//
// An action is DECLARED, not discovered: the panel cannot guess that a
// column called status makes "publish" meaningful. The application names the
// verb, the label an operator reads, and the function that runs it; the
// panel supplies the parts an application should not have to rebuild —
// authorization, tenant and row confinement, the audit entry, and the button.

// ModelAction is one action an application defines for one of its models.
//
// Run receives the ids the operator selected, already confined to what that
// operator may touch: a row of another tenant, or one owned by somebody else
// when the grant is admin:<Model>#own, never reaches it. That confinement is
// the reason the action goes through the panel at all instead of being an
// endpoint of the application's own: an action that bypassed it would be the
// way around every row policy the panel enforces.
type ModelAction struct {
	// Name is the verb on the wire and the RBAC action it is authorized
	// under: a policy reads `p, editors, admin:Post, publish`. It must be
	// unique per model and may not shadow a verb the panel already has.
	Name string
	// Model is the model the action belongs to, by its registry name
	// ("Post"). An unknown model fails at startup rather than leaving the
	// action invisible for the life of the process.
	Model string
	// Label is what the button says ("Publish"). Empty falls back to Name.
	Label string
	// Description is the one-line explanation a UI can show next to it.
	Description string
	// Confirm, when set, is the question the UI asks before running. It is
	// a rendering hint: the panel does not enforce it, because a client
	// that skips the dialog is the same client that could call the
	// endpoint directly.
	Confirm string
	// Destructive marks an action that changes or removes data. It makes
	// the UI treat the button as dangerous, and it makes the action refuse
	// a read-only model, which is what read-only means.
	Destructive bool
	// AllowEmptySelection lets the action run with no rows selected, for
	// the ones whose subject is the table and not a selection ("rebuild the
	// index"). The zero value requires a selection, which is what a button
	// on a grid means. An action offered only on a record always has one,
	// so declaring both refuses to start.
	AllowEmptySelection bool
	// Placement is where the panel offers the action: on the grid's
	// selection (ActionOnSelection, the zero value and what every action
	// did before it could say), on one record — its record view and its
	// row's menu — (ActionOnRecord), or on both
	// (ActionOnSelectionAndRecord). It is enforced, not only drawn: an
	// action offered only on a record is refused at the bulk endpoint, and
	// one offered only on a selection at the record endpoint.
	Placement ActionPlacement
	// Fields are the inputs the action asks for before it runs — the
	// reason for a refund, the date to publish on. The UI draws them as a
	// form in place of the plain confirmation; the panel checks what comes
	// back against them and refuses the call, naming the field, before Run
	// is called; Run reads the values, already typed, from
	// ActionRequest.Input. An action without fields runs exactly as one
	// did before actions could ask.
	Fields []ActionField
	// Run performs the action. Returning an error refuses it: the message
	// reaches the operator, so it should say what a person can do about it.
	Run func(ctx context.Context, req ActionRequest) (ActionResult, error)
}

// ActionRequest is what an action is told about the call.
type ActionRequest struct {
	// Model is the model name the action was invoked on.
	Model string
	// IDs are the selected rows, in the order the client sent them, after
	// tenant and row-ownership confinement. Empty only for an action that
	// declared AllowEmptySelection.
	IDs []string
	// Actor is the operator's username, as the audit trail records it.
	Actor string
	// Tenant is the tenant in force for the request, empty when the panel
	// is not multi-tenant.
	Tenant string
	// Database is the alias of the database the grid was reading, so an
	// action on a model that lives on another handle writes where it read.
	Database string
	// Input is what the operator entered in the action's form, checked
	// against its Fields and typed by them. Nil for an action that declares
	// no fields.
	Input ActionInput
}

// ActionResult is what an action reports back. Everything in it is
// optional: an action that only needs to say "done" returns the zero value.
//
// An action answers in one of three ways. With neither Redirect nor
// Download it answers with a message, as every action did before it could
// do anything else. With Redirect it takes the operator to a page of the
// panel; with Download it hands them a file. It may not set both: the
// panel refuses that answer, and the operator is told the action ran and
// its answer could not be sent.
type ActionResult struct {
	// Message is shown to the operator ("3 posts published"). It is shown
	// with a message and with a redirect; a download's answer is the file.
	Message string
	// Affected is how many rows the action changed, for the audit entry and
	// the toast. It is the action's own count, not the size of the
	// selection.
	Affected int
	// Data carries anything else the UI should get (a summary). It is
	// echoed in a message or redirect answer and NOT audited: an action
	// decides what it returns to the screen, the trail keeps the fact that
	// it ran.
	Data map[string]any
	// Redirect is a page of the panel to take the operator to after the
	// action ran: "/data-studio?model=Invoice&record=42", or
	// "/x/reports/batch/7" for a screen of the application's own. It is a
	// path relative to the panel's root, never a URL: an absolute URL, a
	// scheme, a host, a "//" start, a backslash, a control character or a
	// "." or ".." segment is refused when the action answers, because a
	// redirect an operator's input can steer is how a panel becomes an
	// open redirect.
	Redirect string
	// Download is a file the action produced, sent to the operator as an
	// attachment. The panel sends the bytes the action hands it and
	// nothing else: it never opens a path, so there is no file it can be
	// told to read.
	Download *ActionDownload
}

// reservedActionNames are the verbs the panel owns. An application action
// may not take one: "delete" would be ambiguous at the bulk endpoint, and
// the record verbs are what the panel's own authorization is written in, so
// an action named "update" would be granted by a policy about editing.
var reservedActionNames = func() map[string]struct{} {
	reserved := map[string]struct{}{"delete": {}, "export": {}, "get_schema": {}}
	for _, verb := range recordActions {
		reserved[verb] = struct{}{}
	}
	return reserved
}()

// actionKey identifies an action by model and verb.
type actionKey struct{ model, name string }

// validateModelActions checks the declared actions before the panel starts
// serving, and returns the lookup table the dispatch uses.
//
// Every failure here is a mistake that would otherwise be silent: a typo in
// Model leaves a button that never appears, a duplicate name means one of
// two functions runs and nobody knows which, and a nil Run is a button that
// 500s the first time an operator presses it. Configuration mistakes that
// only show up in production are what this arc keeps finding, so they stop
// the application at startup instead.
func validateModelActions(actions []ModelAction, modelExists func(string) (string, bool)) (map[actionKey]ModelAction, error) {
	table := make(map[actionKey]ModelAction, len(actions))
	for i, action := range actions {
		name := strings.ToLower(strings.TrimSpace(action.Name))
		if name == "" {
			return nil, fmt.Errorf("actions[%d]: Name is required", i)
		}
		if _, reserved := reservedActionNames[name]; reserved {
			return nil, fmt.Errorf("actions[%d] (%s): %q is a verb the panel already uses", i, action.Model, name)
		}
		if strings.TrimSpace(action.Model) == "" {
			return nil, fmt.Errorf("actions[%d] (%s): Model is required", i, name)
		}
		if action.Run == nil {
			return nil, fmt.Errorf("actions[%d] (%s.%s): Run is required", i, action.Model, name)
		}
		canonical, ok := modelExists(action.Model)
		if !ok {
			return nil, fmt.Errorf("actions[%d] (%s): no model named %q in this application", i, name, action.Model)
		}
		key := actionKey{model: canonical, name: name}
		if _, dup := table[key]; dup {
			return nil, fmt.Errorf("actions[%d]: %s already declares an action named %q", i, canonical, name)
		}
		where := fmt.Sprintf("actions[%d] (%s.%s)", i, canonical, name)
		fields, err := validateActionFields(where, action.Fields)
		if err != nil {
			return nil, err
		}
		action.Fields = fields
		placement, err := validateActionPlacement(where, action)
		if err != nil {
			return nil, err
		}
		action.Placement = placement
		action.Name = name
		action.Model = canonical
		if strings.TrimSpace(action.Label) == "" {
			action.Label = action.Name
		}
		table[key] = action
	}
	return table, nil
}

// actionsForModel lists the actions declared on a model, by name, so a
// payload is stable across restarts (map order is not).
func (p *Panel) actionsForModel(model string) []ModelAction {
	if len(p.modelActions) == 0 {
		return nil
	}
	out := make([]ModelAction, 0, 4)
	for key, action := range p.modelActions {
		if key.model == model {
			out = append(out, action)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// actionDescriptor is one action as the schema payload carries it: what the
// UI needs to draw the button and nothing else.
type actionDescriptor struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Confirm     string `json:"confirm,omitempty"`
	Destructive bool   `json:"destructive"`
	// RequiresSelection is the positive form of AllowEmptySelection: a UI
	// asks "do I need rows for this?", not "may I run it without any?".
	RequiresSelection bool `json:"requires_selection"`
	// Fields are the inputs to ask for first; absent when there are none,
	// which is the UI's cue to keep the plain confirmation.
	Fields []actionFieldDescriptor `json:"fields,omitempty"`
	// Placement is where a UI offers the button: "selection",
	// "record" or "selection_and_record". Always present, so a UI never
	// has to guess what a missing value meant.
	Placement ActionPlacement `json:"placement"`
}

// actionDescriptorsFor lists the actions of a model that this operator may
// run. An action they may not run is not in the payload at all: a disabled
// button for a verb that is not theirs tells them about a capability they
// cannot have, and the enforcer is asked again on the call anyway.
func (p *Panel) actionDescriptorsFor(r *http.Request, mi datasource.ModelInfo) []actionDescriptor {
	declared := p.actionsForModel(mi.Name)
	if len(declared) == 0 {
		return nil
	}
	caps := p.capabilitiesFor(r, mi)
	out := make([]actionDescriptor, 0, len(declared))
	for _, action := range declared {
		if !caps.Permissions[action.Name] {
			continue
		}
		// performModelAction refuses a destructive action on a read-only
		// model whoever asks, so it is not a button anybody holds there.
		if action.Destructive && mi.ReadOnly {
			continue
		}
		out = append(out, actionDescriptor{
			Name: action.Name, Label: action.Label, Description: action.Description,
			Confirm: action.Confirm, Destructive: action.Destructive,
			RequiresSelection: !action.AllowEmptySelection,
			Fields:            actionFieldDescriptors(action.Fields),
			Placement:         action.Placement,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// runModelAction dispatches an application-defined action from the bulk
// endpoint. It is the default branch of handleBulkAction: a verb that is
// neither delete nor export is either a declared action or, as before, a
// bad request.
func (p *Panel) runModelAction(c *router.Context, mi datasource.ModelInfo, verb string, ids []string, rawInput json.RawMessage, databaseAlias string) error {
	action, ok := p.modelActions[actionKey{model: mi.Name, name: verb}]
	if !ok {
		return gferrors.BadRequest("unknown action: " + verb)
	}
	return p.performModelAction(c, mi, actionCall{
		action: action, from: ActionOnSelection,
		ids: ids, rawInput: rawInput, database: databaseAlias,
	})
}

// actionCall is one invocation of a declared action, from either place the
// panel offers it: the grid's selection (the bulk endpoint) or one record
// (the record endpoint). Both run through performModelAction, so the
// authorization, the confinement, the input check and the audit entry are
// the same code and cannot drift apart.
type actionCall struct {
	action ModelAction
	// from is where the call came from: ActionOnSelection or
	// ActionOnRecord, never both.
	from     ActionPlacement
	ids      []string
	rawInput json.RawMessage
	database string
}

// performModelAction authorizes, confines, runs and audits one call of a
// declared action, and sends its answer.
func (p *Panel) performModelAction(c *router.Context, mi datasource.ModelInfo, call actionCall) error {
	r := c.Request
	action, ids := call.action, call.ids
	onRecord := call.from == ActionOnRecord

	// The verb IS the permission: a policy that grants publish on
	// admin:Post is what lets this run, and an #own grant confines it the
	// same way it confines a delete.
	rowScope, err := p.authorizeRecordAction(c, mi, action.Name)
	if err != nil {
		return err
	}
	// Where an action is offered is part of its declaration, and the
	// server holds it to it: an action written for one record is not
	// handed a selection of fifty, and one written for a selection is not
	// reachable from a record it was never offered on.
	if !action.Placement.offers(call.from) {
		return actionPlacementRefusal(mi, action, call.from)
	}
	if action.Destructive && mi.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}
	if len(ids) == 0 && !action.AllowEmptySelection {
		return gferrors.BadRequest("ids are required for the " + action.Name + " action")
	}
	// What the operator entered is checked before a row is read, and a
	// value the declaration refuses never reaches Run: the form is a
	// rendering of this check, not a substitute for it.
	input, err := parseActionInput(action, call.rawInput)
	if err != nil {
		return err
	}

	// Same confinement as a bulk delete, and for the same reason: a row
	// this operator cannot see must not be reachable through an action
	// they can run. A refused id is a per-id failure, not a failed
	// request, so a selection that crosses a boundary reports which rows
	// it lost instead of refusing the batch whole.
	type actionError struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	scope := p.requestTenantScope(r, mi)
	allowed := make([]string, 0, len(ids))
	failures := make([]actionError, 0)
	var refusal error
	if len(ids) > 0 {
		st, err := p.src.Store(mi.Name, call.database)
		if err != nil {
			return err
		}
		for _, id := range ids {
			// A record action is about a row that exists: it is read
			// whatever the scope, so a missing id is the 404 the record
			// view would get and not a call over a row that is not there.
			if scope.Enforced() || onRecord {
				if _, err := scopedRecord(r.Context(), st, mi, id, scope); err != nil {
					failures = append(failures, actionError{ID: id, Error: err.Error()})
					refusal = err
					continue
				}
			}
			if err := scopedOwnedRecord(r.Context(), st, mi, id, rowScope); err != nil {
				failures = append(failures, actionError{ID: id, Error: err.Error()})
				refusal = err
				continue
			}
			allowed = append(allowed, id)
		}
	}

	// The audit entry of a record action names the record, so the row's
	// own history (DS-16) shows what was done to it and by whom.
	recordID := ""
	if onRecord && len(ids) == 1 {
		recordID = ids[0]
	}

	// A selection that was entirely refused does not reach Run: an action
	// that asked for three rows and may touch none is not the same call as
	// one invoked over the whole table, and handing it an empty list would
	// make those two indistinguishable.
	if len(ids) > 0 && len(allowed) == 0 {
		p.recordAuditEntry(r, AuditEntry{
			Action:    actionAuditVerb(action.Name),
			ModelName: mi.Name,
			RecordID:  recordID,
			NewValue: map[string]any{
				"requested": len(ids), "affected": 0, "failed": len(failures), "ran": false,
				"on": string(call.from),
			},
		})
		// One record is one answer: the record the operator may not reach
		// is the 404 the record view would give them, not a 200 that says
		// nothing ran.
		if onRecord && refusal != nil {
			return refusal
		}
		return c.JSON(http.StatusOK, map[string]any{
			"action": action.Name, "ran": false,
			"requested": len(ids), "affected": 0, "failed": len(failures), "errors": failures,
			"message": "no selected row is within your scope",
		})
	}

	tenant := ""
	if scope.Enforced() {
		tenant = scope.Tenant
	}
	result, runErr := action.Run(r.Context(), ActionRequest{
		Model:    mi.Name,
		IDs:      allowed,
		Actor:    p.auditActor(r),
		Tenant:   tenant,
		Database: call.database,
		Input:    input,
	})

	// What the action answered is checked before anything is sent: a
	// redirect out of the panel or a download the panel will not send is
	// refused here, and the entry below says so.
	var answer actionAnswer
	var answerErr error
	if runErr == nil {
		answer, answerErr = prepareActionAnswer(result)
	}

	// The trail records the attempt either way. An action that failed
	// halfway still touched rows, so "it errored" is not the same as "it
	// did not run", and only the entry can tell them apart afterwards.
	recorded := map[string]any{
		"requested": len(ids),
		"affected":  result.Affected,
		"failed":    len(failures),
		"ids":       allowed,
		"ran":       true,
		"on":        string(call.from),
	}
	// What the operator entered is part of what happened: "refunded,
	// because the parcel never arrived" is the entry an auditor reads.
	if entered := auditableInput(input); entered != nil {
		recorded["input"] = entered
	}
	if runErr == nil {
		// Which kind of answer the action gave — a message, a page of the
		// panel, a file — and what it named, so "who downloaded the
		// export of these invoices" has an answer.
		answer.audit(recorded)
	}
	switch {
	case runErr != nil:
		recorded["error"] = runErr.Error()
	case answerErr != nil:
		recorded["error"] = answerErr.Error()
	}
	p.recordAuditEntry(r, AuditEntry{
		Action:    actionAuditVerb(action.Name),
		ModelName: mi.Name,
		RecordID:  recordID,
		NewValue:  recorded,
	})

	if runErr != nil {
		// The application's own refusal, in its own words: this is an
		// operator console, and "publish needs a publication date" is
		// worth more than a generic 500.
		return &gferrors.DomainError{
			Code:       "ACTION_FAILED",
			Message:    fmt.Sprintf("%s: %v", action.Label, runErr),
			StatusCode: http.StatusBadRequest,
		}
	}
	if answerErr != nil {
		// The action ran — its rows are changed — and what it answered
		// is not something the panel will send. That is the
		// application's mistake, not the operator's, and the message says
		// both halves so nobody runs it twice thinking it did nothing.
		return &gferrors.DomainError{
			Code:       "ACTION_ANSWER_REFUSED",
			Message:    fmt.Sprintf("%s ran, and its answer was refused: %v", action.Label, answerErr),
			StatusCode: http.StatusInternalServerError,
		}
	}
	if answer.download != nil {
		return answer.download.write(c.Writer)
	}
	payload := map[string]any{
		"action": action.Name, "ran": true,
		"requested": len(ids), "affected": result.Affected,
		"failed": len(failures), "errors": failures,
		"result": answer.kind,
	}
	if result.Message != "" {
		payload["message"] = result.Message
	}
	if answer.redirect != "" {
		payload["redirect"] = answer.redirect
	}
	if len(result.Data) > 0 {
		payload["data"] = result.Data
	}
	return c.JSON(http.StatusOK, payload)
}

// actionAuditVerb is how an application action appears in the trail:
// "action.publish", so a filter on the trail can tell the panel's own verbs
// from the ones an application added without parsing the model name.
func actionAuditVerb(name string) string { return "action." + name }

// ValidateActions checks an application's declared actions against the data
// source they name, and is what turns a mistake in them into a refusal to
// start. orbit's module calls it before the panel is built; NewPanel repeats
// the compilation for callers that wire a panel by hand.
func ValidateActions(src datasource.DataSource, actions []ModelAction) error {
	_, err := validateModelActions(actions, modelResolver(src))
	return err
}

// modelResolver adapts a data source to the canonical-name lookup the
// validation needs. A nil source resolves nothing, so an action on a panel
// with no models is a startup error and not a surprise later.
func modelResolver(src datasource.DataSource) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if src == nil {
			return "", false
		}
		mi, ok := src.Get(name)
		if !ok {
			return "", false
		}
		return mi.Name, true
	}
}

// auditActor is the operator's username for an ActionRequest: the same name
// the trail records, so an action that writes its own log line and the panel
// entry beside it name the same person.
func (p *Panel) auditActor(r *http.Request) string {
	if p == nil || p.config.Auth == nil || r == nil {
		return ""
	}
	user, err := p.authenticatedUser(r)
	if err != nil || user == nil {
		return ""
	}
	return user.Username
}
