package nucleus

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"

	"github.com/jcsvwinston/orbit/internal/datasource"
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
// struct's field types. Primary-key and read-only fields are ignored. This is
// the reflection the panel used to carry; it now lives only here (ADR-001 D2).
func payloadToEntity(meta *model.ModelMeta, rec datasource.Record) (any, error) {
	entityPtr := reflect.New(meta.Type)
	entity := entityPtr.Elem()

	for key, raw := range rec {
		fm, ok := fieldForInput(meta, key)
		if !ok || fm.IsPK || fm.IsReadOnly {
			continue
		}
		field := entity.FieldByName(fm.Name)
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if err := assignInputValue(field, raw); err != nil {
			return nil, fmt.Errorf("invalid value for %s", key)
		}
	}
	return entityPtr.Interface(), nil
}

func fieldForInput(meta *model.ModelMeta, key string) (model.FieldMeta, bool) {
	for _, f := range meta.Fields {
		if strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return f, true
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
		field.SetString(fmt.Sprintf("%v", raw))
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
	return fmt.Errorf("unsupported conversion")
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
	return time.Time{}, fmt.Errorf("invalid time value")
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
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return strconv.ParseInt(fmt.Sprintf("%v", raw), 10, 64)
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
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	default:
		return strconv.ParseUint(fmt.Sprintf("%v", raw), 10, 64)
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
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return strconv.ParseFloat(fmt.Sprintf("%v", raw), 64)
	}
}
