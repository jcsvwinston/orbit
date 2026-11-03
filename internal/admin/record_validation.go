package admin

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
)

// What the record form promises, the panel keeps (OR-78).
//
// The schema the panel serves says which fields are required and of which
// type, and the SPA's form marks them so — but the form sends what it has
// (an input left blank is left out of the payload), and an API client sends
// what it likes. Until the data source refused a record, nothing did: the
// Nucleus adapter validates its models' tags, but a source that does not
// (quarkdatasource before OR-78, an application's own) stored a required
// field empty and answered 201.
//
// validateRecordPayload holds a record to the catalogue before the store is
// asked to write it, and answers what the Nucleus adapter answers — a 422
// with one message per key, in its words — so a client reads the same
// refusal whichever source serves the model. It checks what FieldInfo
// knows and nothing else: a required field named with a value (on a
// create) and not emptied (on an update), and a value of the kind a basic
// Go type declares. What only the source knows stays the source's to check
// — a declared length, a unique key, a validation rule, a key it spells a
// field by that the catalogue does not carry — so a key the panel cannot
// resolve is not refused here.
func (p *Panel) validateRecordPayload(mi datasource.ModelInfo, rec map[string]any, create bool) error {
	problems := map[string]string{}
	named := map[string]bool{} // runtime column -> some key names the field
	unresolved := false
	jsonKeys := p.fieldJSONKeys(mi)
	for key, val := range rec {
		if strings.HasPrefix(key, "_") {
			continue
		}
		col, f, ok := dsResolveField(mi, key)
		if !ok {
			if alias, isAlias := jsonKeys[strings.ToLower(key)]; isAlias {
				col, f, ok = dsResolveField(mi, alias)
			}
		}
		if !ok {
			unresolved = true
			continue
		}
		named[col] = true
		if f.IsPK || f.IsReadOnly {
			continue
		}
		if emptyRecordValue(val) {
			if f.IsRequired {
				problems[key] = "this field is required"
			}
			continue
		}
		if msg := recordValueMismatch(f, val); msg != "" {
			problems[key] = msg
		}
	}
	// A create names every required field. A payload with a key the panel
	// cannot resolve may be naming one under a spelling only the source
	// knows, so the source is left to say what is missing.
	if create && !unresolved {
		for _, f := range mi.Fields {
			if !f.IsRequired || f.IsPK || f.IsReadOnly || f.IsExcluded {
				continue
			}
			if named[runtimeColumn(f.Column)] {
				continue
			}
			problems[runtimeColumn(f.Column)] = "this field is required"
		}
	}
	if len(problems) > 0 {
		return gferrors.ValidationFailed(problems)
	}
	return nil
}

// emptyRecordValue reports whether a payload value is no value: JSON null or
// an empty string (an input left blank).
func emptyRecordValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// recordValueMismatch reports, in the Nucleus adapter's words, a value that
// cannot be one of the kind f declares, or "" when it can or when f declares
// a type the panel does not judge (a struct, a source's own type name). A
// number may arrive as its text — the Nucleus adapter and quarkdatasource
// both read "7" for an integer field — and a timestamp in the layouts both
// read.
func recordValueMismatch(f datasource.FieldInfo, v any) string {
	goType := strings.TrimPrefix(strings.TrimSpace(f.GoType), "*")
	switch goType {
	case "string":
		if _, ok := v.(string); !ok {
			return "must be a string"
		}
	case "int", "int8", "int16", "int32", "int64":
		if !isIntegerValue(v, false) {
			return "must be an integer"
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if !isIntegerValue(v, true) {
			return "must be a non-negative integer"
		}
	case "float32", "float64":
		switch n := v.(type) {
		case float64:
		case string:
			if _, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err != nil {
				return "must be a number"
			}
		default:
			if !isNativeNumber(v) {
				return "must be a number"
			}
		}
	case "bool":
		switch b := v.(type) {
		case bool:
		case string:
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "1", "true", "yes", "on", "0", "false", "no", "off":
			default:
				return "must be a boolean"
			}
		case float64:
			if b != 0 && b != 1 {
				return "must be a boolean"
			}
		default:
			return "must be a boolean"
		}
	case "time.Time":
		s, ok := v.(string)
		if !ok || !parsesAsTimestamp(s) {
			return "must be a timestamp (RFC 3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD)"
		}
	}
	return ""
}

func isIntegerValue(v any, unsigned bool) bool {
	switch n := v.(type) {
	case float64:
		return n == math.Trunc(n) && !math.IsInf(n, 0) && (!unsigned || n >= 0)
	case string:
		if unsigned {
			_, err := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
			return err == nil
		}
		_, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return err == nil
	}
	// A record built in Go rather than decoded from JSON.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return !unsigned || rv.Int() >= 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isNativeNumber(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func parsesAsTimestamp(s string) bool {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}
