// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// What an action asks the operator for before it runs (EXT-01).
//
// A verb over a selection covers the actions that need nothing from the
// person running them. The rest need one more thing — the reason for a
// refund, the date to publish on, the channel to notify — and without a way
// to ask, an application either hard-codes it or writes a screen of its own
// for one input. The application declares the inputs next to the verb; the
// panel draws them as a form, checks what comes back against the
// declaration, and hands the function values that already have their type.
//
// The server is the authority. A client that skips the form is the same
// client that can post to the endpoint directly, so the check is made where
// the function is called, before it is called, and a value it refuses never
// reaches the application.

// ActionFieldType is what an action field asks for.
type ActionFieldType string

// The types an action field may declare. Each one fixes the Go type the
// value arrives with in ActionRequest.Input.
const (
	// ActionFieldText is a line of text; it arrives as a string.
	ActionFieldText ActionFieldType = "text"
	// ActionFieldNumber is a number; it arrives as a float64.
	ActionFieldNumber ActionFieldType = "number"
	// ActionFieldBoolean is a yes/no; it arrives as a bool and is always
	// present, false when the operator left it unticked.
	ActionFieldBoolean ActionFieldType = "boolean"
	// ActionFieldSelect is one of the declared Options; it arrives as the
	// option's Value, a string.
	ActionFieldSelect ActionFieldType = "select"
	// ActionFieldDate is a calendar date, posted as YYYY-MM-DD; it arrives
	// as a time.Time at midnight UTC.
	ActionFieldDate ActionFieldType = "date"
)

// actionFieldTypes is the vocabulary, in the order an error lists it.
var actionFieldTypes = []ActionFieldType{
	ActionFieldText, ActionFieldNumber, ActionFieldBoolean, ActionFieldSelect, ActionFieldDate,
}

// ActionField is one input an action asks for before it runs.
type ActionField struct {
	// Name is the key the value travels under, on the wire and in
	// ActionRequest.Input. It starts with a letter and holds letters,
	// digits, '_' and '-', and is unique within the action.
	Name string
	// Label is what the form says next to the input. Empty falls back to
	// Name.
	Label string
	// Type is what the operator enters. Empty means ActionFieldText; a type
	// the panel does not know refuses to start.
	Type ActionFieldType
	// Required refuses the call when the value is missing or blank. On a
	// boolean it means the box must be ticked ("I understand this cannot be
	// undone").
	Required bool
	// Help is the one line a form shows under the input.
	Help string
	// Options are the choices of a select: the value posted and the label
	// read. A select needs at least one; any other type may not have them.
	Options []ActionOption
}

// ActionOption is one choice of a select field.
type ActionOption struct {
	// Value is what is posted and what Run receives. Unique within the
	// field and never empty.
	Value string
	// Label is what the operator reads. Empty falls back to Value.
	Label string
}

// ActionInput is what the operator entered in an action's form, already
// checked against its Fields and typed by them: text and select as string,
// number as float64, boolean as bool, date as time.Time. A text, number,
// select or date field left empty is absent; a boolean is always present.
// It is nil for an action that declares no fields.
type ActionInput map[string]any

// String returns a text or select value, or "" when it is absent.
func (in ActionInput) String(name string) string {
	s, _ := in[name].(string)
	return s
}

// Number returns a number value and whether it was entered.
func (in ActionInput) Number(name string) (float64, bool) {
	n, ok := in[name].(float64)
	return n, ok
}

// Bool returns a boolean value; false when it is absent.
func (in ActionInput) Bool(name string) bool {
	b, _ := in[name].(bool)
	return b
}

// Date returns a date value and whether it was entered.
func (in ActionInput) Date(name string) (time.Time, bool) {
	d, ok := in[name].(time.Time)
	return d, ok
}

// actionFieldName is the shape a field name may take: it becomes a JSON key
// and the id of an input in the form, so it is kept to what both read
// without escaping.
var actionFieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// actionDateLayout is the one form a date is posted in: what an HTML date
// input produces, and what has no time zone to misread.
const actionDateLayout = "2006-01-02"

// validateActionFields checks an action's declared inputs and returns them
// normalised (types and labels filled in). where names the action in the
// error, the way validateModelActions names it.
func validateActionFields(where string, fields []ActionField) ([]ActionField, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	out := make([]ActionField, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for i, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			return nil, fmt.Errorf("%s: fields[%d]: Name is required", where, i)
		}
		if !actionFieldName.MatchString(name) {
			return nil, fmt.Errorf("%s: field %q: a name starts with a letter and holds letters, digits, '_' and '-'", where, name)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("%s: field %q is declared twice", where, name)
		}
		seen[name] = struct{}{}

		typ := ActionFieldType(strings.ToLower(strings.TrimSpace(string(field.Type))))
		if typ == "" {
			typ = ActionFieldText
		}
		if !knownActionFieldType(typ) {
			return nil, fmt.Errorf("%s: field %q: unknown type %q (the panel draws %s)", where, name, field.Type, actionFieldTypeList())
		}

		if typ == ActionFieldSelect {
			if len(field.Options) == 0 {
				return nil, fmt.Errorf("%s: field %q: a select needs at least one option", where, name)
			}
			options := make([]ActionOption, 0, len(field.Options))
			values := make(map[string]struct{}, len(field.Options))
			for j, option := range field.Options {
				if option.Value == "" {
					return nil, fmt.Errorf("%s: field %q: options[%d] has no Value", where, name, j)
				}
				if _, dup := values[option.Value]; dup {
					return nil, fmt.Errorf("%s: field %q: option %q is declared twice", where, name, option.Value)
				}
				values[option.Value] = struct{}{}
				if strings.TrimSpace(option.Label) == "" {
					option.Label = option.Value
				}
				options = append(options, option)
			}
			field.Options = options
		} else if len(field.Options) > 0 {
			return nil, fmt.Errorf("%s: field %q: options belong to a select, and this field is a %s", where, name, typ)
		}

		field.Name = name
		field.Type = typ
		if strings.TrimSpace(field.Label) == "" {
			field.Label = name
		}
		out = append(out, field)
	}
	return out, nil
}

func knownActionFieldType(typ ActionFieldType) bool {
	for _, known := range actionFieldTypes {
		if typ == known {
			return true
		}
	}
	return false
}

func actionFieldTypeList() string {
	names := make([]string, len(actionFieldTypes))
	for i, typ := range actionFieldTypes {
		names[i] = string(typ)
	}
	return strings.Join(names, ", ")
}

// actionFieldDescriptor is one field as the schema payload carries it: what
// a form needs to draw the input and nothing else.
type actionFieldDescriptor struct {
	Name     string                   `json:"name"`
	Label    string                   `json:"label"`
	Type     ActionFieldType          `json:"type"`
	Required bool                     `json:"required"`
	Help     string                   `json:"help,omitempty"`
	Options  []actionOptionDescriptor `json:"options,omitempty"`
}

type actionOptionDescriptor struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func actionFieldDescriptors(fields []ActionField) []actionFieldDescriptor {
	if len(fields) == 0 {
		return nil
	}
	out := make([]actionFieldDescriptor, 0, len(fields))
	for _, field := range fields {
		d := actionFieldDescriptor{
			Name: field.Name, Label: field.Label, Type: field.Type,
			Required: field.Required, Help: field.Help,
		}
		for _, option := range field.Options {
			d.Options = append(d.Options, actionOptionDescriptor{Value: option.Value, Label: option.Label})
		}
		out = append(out, d)
	}
	return out
}

// parseActionInput checks what a client posted against an action's fields
// and returns it typed. Every field that fails is named in one answer — a
// form that learns its mistakes one round trip at a time is a form that
// takes five submits — as a 422 whose details map each field to what is
// wrong with it.
func parseActionInput(action ModelAction, raw json.RawMessage) (ActionInput, error) {
	if len(action.Fields) == 0 {
		// An action that asks for nothing behaves as it did before it could
		// ask: whatever came with the call is not its business.
		return nil, nil
	}

	posted := map[string]json.RawMessage{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if trimmed[0] != '{' {
			return nil, gferrors.BadRequest("input must be an object of field values")
		}
		if err := json.Unmarshal(trimmed, &posted); err != nil {
			return nil, gferrors.BadRequest("input must be an object of field values")
		}
	}

	input := ActionInput{}
	problems := map[string]string{}
	declared := make(map[string]struct{}, len(action.Fields))
	for _, field := range action.Fields {
		declared[field.Name] = struct{}{}
		value, problem := parseActionValue(field, posted[field.Name])
		if problem != "" {
			problems[field.Name] = problem
			continue
		}
		if value != nil {
			input[field.Name] = value
		}
	}
	for name := range posted {
		if _, ok := declared[name]; !ok {
			problems[name] = "is not a field of this action"
		}
	}
	if len(problems) > 0 {
		return nil, actionInputRefusal(action, problems)
	}
	return input, nil
}

// parseActionValue reads one field's value. It returns nil and no problem
// for an optional field left empty.
func parseActionValue(field ActionField, raw json.RawMessage) (any, string) {
	var value any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, "is not a value"
		}
	}

	if field.Type == ActionFieldBoolean {
		ticked := false
		switch v := value.(type) {
		case nil:
		case bool:
			ticked = v
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "on", "1":
				ticked = true
			case "false", "off", "0", "":
			default:
				return nil, "must be true or false"
			}
		default:
			return nil, "must be true or false"
		}
		if field.Required && !ticked {
			return nil, "must be ticked"
		}
		return ticked, ""
	}

	// Every other type: missing, null and blank text are all "empty".
	if value == nil {
		if field.Required {
			return nil, "is required"
		}
		return nil, ""
	}
	if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
		if field.Required {
			return nil, "is required"
		}
		return nil, ""
	}

	switch field.Type {
	case ActionFieldText:
		s, ok := value.(string)
		if !ok {
			return nil, "must be text"
		}
		return s, ""
	case ActionFieldNumber:
		switch v := value.(type) {
		case float64:
			return v, ""
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, "must be a number"
			}
			return n, ""
		}
		return nil, "must be a number"
	case ActionFieldSelect:
		s, ok := value.(string)
		if !ok {
			return nil, "must be one of " + optionValues(field.Options)
		}
		for _, option := range field.Options {
			if option.Value == s {
				return s, ""
			}
		}
		return nil, "must be one of " + optionValues(field.Options)
	case ActionFieldDate:
		s, ok := value.(string)
		if !ok {
			return nil, "must be a date (YYYY-MM-DD)"
		}
		d, err := time.ParseInLocation(actionDateLayout, strings.TrimSpace(s), time.UTC)
		if err != nil {
			return nil, "must be a date (YYYY-MM-DD)"
		}
		return d, ""
	}
	// validateActionFields admits no other type; reaching here is a bug.
	return nil, "has a type the panel cannot read"
}

func optionValues(options []ActionOption) string {
	values := make([]string, len(options))
	for i, option := range options {
		values[i] = option.Value
	}
	return strings.Join(values, ", ")
}

// actionInputRefusal is the 422 a form reads its field errors from. The
// message names every field too, so a client that only shows the message
// still says which input to fix.
func actionInputRefusal(action ModelAction, problems map[string]string) error {
	names := make([]string, 0, len(problems))
	for name := range problems {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + " " + problems[name]
	}
	return &gferrors.DomainError{
		Code:       "VALIDATION_FAILED",
		Message:    fmt.Sprintf("%s: %s", action.Label, strings.Join(parts, "; ")),
		StatusCode: http.StatusUnprocessableEntity,
		Details:    problems,
	}
}

// auditableInput is the input as the trail records it: dates as the day an
// operator picked rather than a timestamp with a zone they never chose.
func auditableInput(input ActionInput) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for name, value := range input {
		if d, ok := value.(time.Time); ok {
			out[name] = d.Format(actionDateLayout)
			continue
		}
		out[name] = value
	}
	return out
}
