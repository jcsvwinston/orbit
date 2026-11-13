package quarkdatasource

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
)

// What a record may carry, and how its values become the model's.
//
// The Nucleus adapter refuses a record it cannot store faithfully with one
// 422 that names every offending key — a key that names no field, a value
// whose type does not fit, a field named twice, a failed validation rule —
// and writes nothing. This adapter used to hand the record to json.Unmarshal
// and the row to Quark: an unknown key was dropped without a word, a value of
// the wrong type reached the operator as a 500 carrying the decoder's text, a
// required field left out was stored empty. Data Studio over Quark now gets
// the Nucleus adapter's answer, in the same shape (gferrors.ValidationFailed,
// one message per key), and the messages are the Nucleus adapter's where the
// check is the same one.

// Messages shared with the Nucleus adapter (datasource/nucleus) and the
// framework validator, so a client reads the same text whichever adapter
// serves the model.
const (
	msgRequired = "this field is required"
	msgUnknown  = "unknown field"
)

// fieldValue is one schema field a record names, with the value coerced to
// the field's Go type.
type fieldValue struct {
	key    string // the record key that named the field
	column string // its storage column
	index  int    // its index in the struct
	unique bool   // the column carries a unique constraint
	value  reflect.Value
	null   bool // the record named the field with no value
}

// readRecord resolves every key of rec to a schema field and coerces its
// value to the field's type. A create resolves a key by column, Go name or
// the struct's JSON key; an update by column or Go name only (see
// recordToColumnMap for why). Keys with a leading underscore are metadata —
// the "_model" a multi-model export writes — and are skipped, as the Nucleus
// adapter skips them. The primary key and read-only (version) fields are
// recognised and left out: the database owns them.
//
// Every problem is collected before any is reported, keys visited in sorted
// order so the key a duplicate is reported under is deterministic.
func (s *store[T]) readRecord(rec datasource.Record, update bool) ([]fieldValue, map[string]string) {
	problems := map[string]string{}
	named := make(map[string]string, len(rec)) // column -> first key naming it
	keys := make([]string, 0, len(rec))
	for k := range rec {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	values := make([]fieldValue, 0, len(rec))
	for _, k := range keys {
		if strings.HasPrefix(k, "_") {
			continue
		}
		fi, ok := s.info.Field(k)
		if !ok {
			alias, isAlias := s.fieldForInput(k)
			switch {
			case !isAlias:
				problems[k] = msgUnknown
				continue
			case update:
				problems[k] = fmt.Sprintf("an update names this field by its column (%s)", alias.Column)
				continue
			}
			fi = alias
		}
		if first, dup := named[fi.Column]; dup {
			problems[k] = fmt.Sprintf("names the same field as %q", first)
			continue
		}
		named[fi.Column] = k
		// A hidden column is not a field of the panel at all (the schema
		// never emitted it, so no client names it honestly): answered as a
		// column the model does not have, as the panel answers a filter by
		// one. A read-only field is left to the database, as the key is.
		if fi.IsExcluded {
			problems[k] = msgUnknown
			continue
		}
		if fi.IsPK || fi.IsReadOnly {
			continue
		}
		fm, ok := s.meta.FieldByCol[strings.ToLower(fi.Column)]
		if !ok {
			problems[k] = msgUnknown
			continue
		}
		v, null, err := coerceInput(s.typ.Field(fm.Index).Type, fm.Size, rec[k])
		if err != nil {
			problems[k] = err.Error()
			continue
		}
		if fi.IsRequired && (null || isBlankString(v)) {
			problems[k] = msgRequired
			continue
		}
		values = append(values, fieldValue{key: k, column: fi.Column, index: fm.Index, unique: fm.Unique, value: v, null: null})
	}

	// A create names every required field. A field the record leaves out
	// would be stored as the Go zero value — an empty title under a schema
	// that says the title is required — which is the row the operator was
	// promised could not exist.
	if !update {
		for _, f := range s.info.Fields {
			// A required column the model hides cannot be named here, and
			// is stored as its Go zero value — the Nucleus adapter's
			// answer too (the panel's own create check skips it).
			if !f.IsRequired || f.IsPK || f.IsReadOnly || f.IsExcluded {
				continue
			}
			if _, ok := named[f.Column]; ok {
				continue
			}
			problems[f.Column] = msgRequired
		}
	}
	return values, problems
}

func isBlankString(v reflect.Value) bool {
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.IsValid() && v.Kind() == reflect.String && v.Len() == 0
}

// validationErr is the 422 the Nucleus adapter answers a record it refuses
// with: one message per offending key.
func validationErr(problems map[string]string) error {
	if len(problems) == 0 {
		return nil
	}
	return gferrors.ValidationFailed(problems)
}

var timeType = reflect.TypeOf(time.Time{})

// coerceInput converts a record value to t, the struct field's type. It
// reports null when the record names the field with no value: JSON null, or
// an empty string for a field that is not text (the empty cell of a CSV, an
// input left blank) — the field then keeps its zero value on a create and is
// written as NULL on an update.
//
// The scalar kinds are coerced the way the Nucleus adapter coerces them (a
// number or its text for a numeric field, only text for a text field), with
// the width of the type and the column's declared size checked rather than
// left to the driver. A timestamp is read in the layouts the Nucleus adapter
// reads. Any other type is the model's own and is decoded the way
// encoding/json decodes it.
func coerceInput(t reflect.Type, size int, raw any) (reflect.Value, bool, error) {
	if raw == nil {
		return reflect.Value{}, true, nil
	}
	if s, ok := raw.(string); ok && s == "" && !isTextual(t) {
		return reflect.Value{}, true, nil
	}
	if t.Kind() == reflect.Pointer {
		v, null, err := coerceInput(t.Elem(), size, raw)
		if err != nil || null {
			return reflect.Value{}, null, err
		}
		ptr := reflect.New(t.Elem())
		ptr.Elem().Set(v)
		return ptr, false, nil
	}
	if t == timeType {
		ts, err := parseTimeInput(raw)
		if err != nil {
			return reflect.Value{}, false, err
		}
		return reflect.ValueOf(ts), false, nil
	}

	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		str, ok := raw.(string)
		if !ok {
			return reflect.Value{}, false, errors.New("must be a string")
		}
		if size > 0 && utf8.RuneCountInString(str) > size {
			return reflect.Value{}, false, fmt.Errorf("must be at most %d characters", size)
		}
		v.SetString(str)
	case reflect.Bool:
		b, err := toBool(raw)
		if err != nil {
			return reflect.Value{}, false, err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt(raw)
		if err != nil {
			return reflect.Value{}, false, err
		}
		if v.OverflowInt(n) {
			return reflect.Value{}, false, fmt.Errorf("out of range for %s", t.Kind())
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := toUint(raw)
		if err != nil {
			return reflect.Value{}, false, err
		}
		if v.OverflowUint(n) {
			return reflect.Value{}, false, fmt.Errorf("out of range for %s", t.Kind())
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := toFloat(raw)
		if err != nil {
			return reflect.Value{}, false, err
		}
		if v.OverflowFloat(f) {
			return reflect.Value{}, false, fmt.Errorf("out of range for %s", t.Kind())
		}
		v.SetFloat(f)
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return reflect.Value{}, false, fmt.Errorf("must be a value of type %s", t)
		}
		ptr := reflect.New(t)
		if err := json.Unmarshal(data, ptr.Interface()); err != nil {
			return reflect.Value{}, false, fmt.Errorf("must be a value of type %s", t)
		}
		return ptr.Elem(), false, nil
	}
	return v, false, nil
}

// isTextual reports whether an empty string is a value of t rather than the
// absence of one: a text field, or a type that decodes from text itself.
func isTextual(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.String {
		return true
	}
	if t == timeType {
		return false
	}
	return reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// parseTimeInput reads a timestamp in the layouts the Nucleus adapter reads
// (the SPA's datetime input sends RFC 3339; a CSV often carries a bare date).
func parseTimeInput(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		v = strings.TrimSpace(v)
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
			if ts, err := time.Parse(layout, v); err == nil {
				return ts, nil
			}
		}
	}
	return time.Time{}, errors.New("must be a timestamp (RFC 3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD)")
}

func toBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off":
			return false, nil
		}
	default:
		if n, ok := toInt64(raw); ok && (n == 0 || n == 1) {
			if f, isFloat := raw.(float64); !isFloat || f == math.Trunc(f) {
				return n == 1, nil
			}
		}
	}
	return false, errors.New("must be a boolean")
}

func toInt(raw any) (int64, error) {
	switch v := raw.(type) {
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, errors.New("must be an integer")
		}
		return n, nil
	case float64:
		if v != math.Trunc(v) || v < math.MinInt64 || v >= math.MaxInt64 {
			return 0, errors.New("must be an integer")
		}
		return int64(v), nil
	case float32:
		return toInt(float64(v))
	case uint, uint64:
		n := reflect.ValueOf(v).Uint()
		if n > math.MaxInt64 {
			return 0, errors.New("must be an integer")
		}
		return int64(n), nil
	}
	if n, ok := toInt64(raw); ok {
		return n, nil
	}
	return 0, errors.New("must be an integer")
}

func toUint(raw any) (uint64, error) {
	const msg = "must be a non-negative integer"
	switch v := raw.(type) {
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, errors.New(msg)
		}
		return n, nil
	case float64:
		if v != math.Trunc(v) || v < 0 || v >= math.MaxUint64 {
			return 0, errors.New(msg)
		}
		return uint64(v), nil
	case float32:
		return toUint(float64(v))
	case uint, uint8, uint16, uint32, uint64:
		return reflect.ValueOf(v).Uint(), nil
	}
	if n, ok := toInt64(raw); ok && n >= 0 {
		return uint64(n), nil
	}
	return 0, errors.New(msg)
}

func toFloat(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, errors.New("must be a number")
		}
		return f, nil
	}
	if n, ok := toInt64(raw); ok {
		return float64(n), nil
	}
	return 0, errors.New("must be a number")
}

// toInt64 widens any native integer (or integral float64, the JSON shape) to
// int64.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	default:
		return 0, false
	}
}

// bindValue is what UpdateMap binds for a coerced field value: the scalar
// itself for the basic kinds (what the column takes), the value of the
// field's own type otherwise (a quark.Nullable, a uuid, a JSON column — each
// a driver.Valuer), and nil for NULL.
func bindValue(fv fieldValue) any {
	if fv.null {
		return nil
	}
	v := fv.value
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Type() == timeType {
		return v.Interface()
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return fv.value.Interface()
}

// stampedByQuark reports whether Quark fills the field itself on a create
// when the entity leaves it zero: the created_at/updated_at convention
// (quark DX-20) on a time.Time field. Such a field is not one the operator
// has to name, whatever its NOT NULL says.
func stampedByQuark(f quark.FieldMeta, t reflect.Type) bool {
	if t != timeType {
		return false
	}
	return strings.EqualFold(f.Column, "created_at") || strings.EqualFold(f.Column, "updated_at")
}
