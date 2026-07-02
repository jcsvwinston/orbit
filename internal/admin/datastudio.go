package admin

import (
	"fmt"
	"net/url"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/internal/datasource"
)

// This file holds Data Studio helpers rewritten over the neutral datasource
// contract (ADR-001): field resolution, filter collection, order-by validation,
// and record-value access. They replace the old model.ModelMeta/FieldMeta
// versions in handlers.go.

// recordValue reads a field's value from a neutral Record, tolerating whichever
// key the backend used (storage column or Go name — the same lookup the SPA's
// readField performs).
func recordValue(rec datasource.Record, f datasource.FieldInfo) (any, bool) {
	for _, key := range []string{f.Column, runtimeColumn(f.Column), f.Name} {
		if key == "" {
			continue
		}
		if v, ok := rec[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// dsResolveField finds the field whose runtime column, storage column, or Go
// name matches key (case-insensitive) and returns its runtime column.
func dsResolveField(mi datasource.ModelInfo, key string) (column string, field datasource.FieldInfo, ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", datasource.FieldInfo{}, false
	}
	for _, f := range mi.Fields {
		col := runtimeColumn(f.Column)
		if strings.EqualFold(key, col) || strings.EqualFold(key, f.Column) || strings.EqualFold(key, f.Name) {
			return col, f, true
		}
	}
	return "", datasource.FieldInfo{}, false
}

// dsCollectFilters extracts exact-match filters from a query string, skipping
// the reserved pagination/selection params, and validates each against the
// model's filterable fields.
func dsCollectFilters(mi datasource.ModelInfo, values url.Values) (map[string]string, error) {
	filters := make(map[string]string)
	for key, vals := range values {
		switch key {
		case "page", "page_size", "search", "order_by", "db", "database", "db_alias":
			continue
		}
		if len(vals) == 0 {
			continue
		}
		raw := strings.TrimSpace(vals[0])
		if raw == "" {
			continue
		}
		col, normalized, err := dsNormalizeFilter(mi, key, raw)
		if err != nil {
			return nil, err
		}
		filters[col] = normalized
	}
	return filters, nil
}

func dsNormalizeFilter(mi datasource.ModelInfo, key, value string) (column, normalized string, err error) {
	col, field, found := dsResolveField(mi, key)
	if !found {
		return "", "", gferrors.BadRequest(fmt.Sprintf("invalid filter field %q", key))
	}
	if !field.IsFilter {
		return "", "", gferrors.BadRequest(fmt.Sprintf("filter is not enabled for %q", key))
	}
	normalized = value
	if strings.EqualFold(field.GoType, "bool") {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			normalized = "1"
		case "0", "false", "no", "off":
			normalized = "0"
		default:
			return "", "", gferrors.BadRequest(fmt.Sprintf("invalid boolean value %q for filter %q", value, key))
		}
	}
	return col, normalized, nil
}

// dsSanitizeOrderBy validates a user order-by expression against the model's
// columns and returns a safe "col dir[, col dir ...]" clause. It mirrors the
// allow-list semantics of the framework's model.SanitizeOrderBy, reimplemented
// over ModelInfo so the panel does not import pkg/model. The synthetic primary
// key "id" is always accepted; unknown columns and bad directions are rejected.
func dsSanitizeOrderBy(mi datasource.ModelInfo, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, clause := range parts {
		fields := strings.Fields(strings.TrimSpace(clause))
		if len(fields) == 0 {
			continue
		}
		col, ok := resolveOrderColumn(mi, fields[0])
		if !ok {
			return "", gferrors.BadRequest("invalid order_by")
		}
		dir := "asc"
		if len(fields) > 1 {
			switch strings.ToLower(fields[1]) {
			case "asc":
				dir = "asc"
			case "desc":
				dir = "desc"
			default:
				return "", gferrors.BadRequest("invalid order_by")
			}
		}
		if len(fields) > 2 {
			return "", gferrors.BadRequest("invalid order_by")
		}
		out = append(out, col+" "+dir)
	}
	return strings.Join(out, ", "), nil
}

// resolveOrderColumn accepts a model field's column or Go name (case-insensitive)
// and the synthetic "id"; it returns the runtime column to sort by.
func resolveOrderColumn(mi datasource.ModelInfo, key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	if strings.EqualFold(key, "id") {
		return "id", true
	}
	if col, _, ok := dsResolveField(mi, key); ok {
		return col, true
	}
	return "", false
}
