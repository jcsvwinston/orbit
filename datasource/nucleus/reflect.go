package nucleus

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/model"

	"github.com/jcsvwinston/orbit/datasource"
)

// entityToRecord converts a Nucleus entity (struct or *struct) to a neutral
// Record via a JSON round-trip. This is deliberate: it reproduces byte-for-byte
// the JSON the panel used to emit when it forwarded entities straight to
// c.JSON, so the embedded SPA reads records unchanged (ADR-001 O3).
func entityToRecord(entity any) (datasource.Record, error) {
	if entity == nil {
		return nil, nil
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("datasource/nucleus: marshal entity: %w", err)
	}
	var rec datasource.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("datasource/nucleus: unmarshal entity: %w", err)
	}
	return rec, nil
}

// payloadToEntity builds a new *entity from a Record, coercing values into the
// struct's field types. Primary-key and read-only fields are ignored, as are
// metadata keys (a leading underscore, e.g. the "_model" tag of a multi-model
// export). This is the reflection the panel used to carry; it now lives only
// here (ADR-001 D2).
//
// Two classes of input are refused with a per-field 422 instead of being
// silently absorbed, because an admin that writes whatever it is sent
// corrupts data: a key that names no field of the model, and a value whose
// JSON type does not fit the field (a number for a string field used to be
// stored as "123.0").
func payloadToEntity(meta *model.ModelMeta, rec datasource.Record) (any, error) {
	entityPtr := reflect.New(meta.Type)
	if _, err := applyPayload(meta, entityPtr.Elem(), rec); err != nil {
		return nil, err
	}
	return entityPtr.Interface(), nil
}

// applyPayload assigns rec onto entity (a settable struct value) and returns
// the set of fields it touched, keyed by field meta. Validation problems come
// back as one gferrors.ValidationFailed carrying every offending key.
//
// A field named more than once — under its column and its json tag, or under
// two letter cases — is a problem, not a race: the keys used to be applied in
// map order, so which value won was a coin flip, and a caller that stamps one
// key (the panel's tenant) could be outvoted by another it did not know.
// Keys are visited in sorted order so the key reported is deterministic.
func applyPayload(meta *model.ModelMeta, entity reflect.Value, rec datasource.Record) (map[string]model.FieldMeta, error) {
	touched := make(map[string]model.FieldMeta, len(rec))
	problems := map[string]string{}
	named := make(map[string]string, len(rec)) // field name -> first key naming it
	keys := make([]string, 0, len(rec))
	for key := range rec {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw := rec[key]
		if strings.HasPrefix(key, "_") {
			continue
		}
		fm, ok := fieldForInput(meta, key)
		if !ok {
			problems[key] = "unknown field"
			continue
		}
		if first, dup := named[fm.Name]; dup {
			problems[key] = fmt.Sprintf("names the same field as %q", first)
			continue
		}
		named[fm.Name] = key
		if fm.IsPK || fm.IsReadOnly {
			continue
		}
		field := entity.FieldByName(fm.Name)
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if err := assignInputValue(field, raw); err != nil {
			problems[key] = err.Error()
			continue
		}
		touched[fm.Name] = fm
	}
	if len(problems) > 0 {
		return nil, gferrors.ValidationFailed(problems)
	}
	return touched, nil
}

// fieldForInput resolves a payload key to a field: by storage column, by Go
// field name, or by the field's json tag (what entityToRecord emits, so a
// record read from the panel round-trips unchanged).
func fieldForInput(meta *model.ModelMeta, key string) (model.FieldMeta, bool) {
	for _, f := range meta.Fields {
		if strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return f, true
		}
	}
	if meta.Type != nil {
		for _, f := range meta.Fields {
			sf, ok := meta.Type.FieldByName(f.Name)
			if !ok {
				continue
			}
			tag, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
			if tag != "" && tag != "-" && strings.EqualFold(key, tag) {
				return f, true
			}
		}
	}
	return model.FieldMeta{}, false
}

// assignInputValue coerces a decoded JSON value into a struct field. It mirrors
// the coercion the panel previously performed in handlers.go.
func assignInputValue(field reflect.Value, raw any) error {
	if raw == nil {
		return nil
	}

	fieldType := field.Type()
	if fieldType.Kind() == reflect.Ptr {
		ptr := reflect.New(fieldType.Elem())
		if err := assignInputValue(ptr.Elem(), raw); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	}

	if isTimeType(fieldType) {
		ts, err := parseTimeValue(raw)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(ts))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		// Only a JSON string fits a string field. Coercing numbers,
		// booleans or objects via %v loses the type silently (123 became
		// "123.0" through float64), so refuse them.
		str, ok := raw.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		field.SetString(str)
		return nil
	case reflect.Bool:
		if v, ok := raw.(bool); ok {
			field.SetBool(v)
			return nil
		}
		s := strings.ToLower(fmt.Sprintf("%v", raw))
		field.SetBool(s == "1" || s == "true" || s == "yes" || s == "on")
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := toInt64(raw)
		if err != nil {
			return err
		}
		field.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := toUint64(raw)
		if err != nil {
			return err
		}
		field.SetUint(n)
		return nil
	case reflect.Float32, reflect.Float64:
		f, err := toFloat64(raw)
		if err != nil {
			return err
		}
		field.SetFloat(f)
		return nil
	}

	val := reflect.ValueOf(raw)
	if val.Type().AssignableTo(fieldType) {
		field.Set(val)
		return nil
	}
	if val.Type().ConvertibleTo(fieldType) {
		field.Set(val.Convert(fieldType))
		return nil
	}
	return fmt.Errorf("cannot assign a %s to a %s field", val.Type(), fieldType)
}

func isTimeType(t reflect.Type) bool {
	return t.PkgPath() == "time" && t.Name() == "Time"
}

func parseTimeValue(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}, nil
		}
		layouts := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"}
		for _, layout := range layouts {
			if ts, err := time.Parse(layout, v); err == nil {
				return ts, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("must be a timestamp (RFC 3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD)")
}

func toInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("must be an integer")
		}
		return n, nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

func toUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative value for unsigned field")
		}
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative value for unsigned field")
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative value for unsigned field")
		}
		return uint64(v), nil
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("must be a non-negative integer")
		}
		return n, nil
	default:
		return 0, fmt.Errorf("must be a non-negative integer")
	}
}

func toFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("must be a number")
		}
		return f, nil
	default:
		return 0, fmt.Errorf("must be a number")
	}
}
