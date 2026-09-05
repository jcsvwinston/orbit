package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
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
		// "tenant" is the explicit scope override consumed by
		// tenantContextMiddleware (see requestTenant), not a field filter;
		// before it was reserved here every ?tenant= list request answered
		// 400 "invalid filter field".
		case "page", "page_size", "search", "order_by", "db", "database", "db_alias", "tenant":
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

// canonicalID renders a record's key (or any scalar the panel compares by
// identity, such as a tenant value) the way ids cross the API boundary: as
// the string of ADR-001 D1. Records come out of a JSON round-trip, so an
// integer key arrives as float64 — 3, 3.0 and "3" all become "3"; strings
// are trimmed; anything with a textual form (uuid.UUID, json.Number) uses
// it. It returns false for nil and for values with no usable text.
//
// Integer keys beyond 2^53 lose precision in the float64 round-trip before
// they get here; that is a property of the JSON records, not of this
// function.
func canonicalID(v any) (string, bool) {
	switch n := v.(type) {
	case nil:
		return "", false
	case string:
		s := strings.TrimSpace(n)
		return s, s != ""
	case []byte:
		s := strings.TrimSpace(string(n))
		return s, s != ""
	case json.Number:
		return n.String(), n.String() != ""
	case float64:
		return canonicalFloatID(n)
	case float32:
		return canonicalFloatID(float64(n))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", n), true
	case fmt.Stringer:
		s := strings.TrimSpace(n.String())
		return s, s != ""
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", n))
		return s, s != ""
	}
}

func canonicalFloatID(f float64) (string, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", false
	}
	if f == math.Trunc(f) && math.Abs(f) < (1<<63) {
		return strconv.FormatInt(int64(f), 10), true
	}
	return strconv.FormatFloat(f, 'f', -1, 64), true
}

// bulkMaxIDs bounds one bulk request. The SPA selects rows from a grid page
// of at most 200; a client sending more than this is not a grid.
const bulkMaxIDs = 1000

// decodeRecordIDs turns the ids of a bulk request into boundary strings
// (ADR-001 D1). Each id may be a JSON string or a JSON number — the SPA
// sends strings, older clients sent numbers — and nothing else: null,
// objects, arrays and booleans are refused with a message naming the
// position, instead of the "invalid JSON" a []uint decode used to answer
// for every string id (a UUID, or even "7").
func decodeRecordIDs(raw []json.RawMessage) ([]string, error) {
	if len(raw) > bulkMaxIDs {
		return nil, gferrors.BadRequest(fmt.Sprintf("too many ids: %d (max %d)", len(raw), bulkMaxIDs))
	}
	ids := make([]string, 0, len(raw))
	for i, token := range raw {
		t := bytes.TrimSpace(token)
		switch {
		case len(t) > 0 && t[0] == '"':
			var s string
			if err := json.Unmarshal(t, &s); err != nil {
				return nil, gferrors.BadRequest(fmt.Sprintf("ids[%d]: %v", i, err))
			}
			s = strings.TrimSpace(s)
			if s == "" {
				return nil, gferrors.BadRequest(fmt.Sprintf("ids[%d] must not be empty", i))
			}
			ids = append(ids, s)
		case len(t) > 0 && (t[0] == '-' || (t[0] >= '0' && t[0] <= '9')):
			var n json.Number
			if err := json.Unmarshal(t, &n); err != nil {
				return nil, gferrors.BadRequest(fmt.Sprintf("ids[%d]: %v", i, err))
			}
			ids = append(ids, n.String())
		default:
			return nil, gferrors.BadRequest(fmt.Sprintf("ids[%d] must be a string or a number", i))
		}
	}
	return ids, nil
}

// modelSearchable reports whether ?search= has any column to look in: a
// field the backend marks searchable (Nucleus: admin:"search",
// ModelConfig.SearchFields or the Field settings editor; Quark: every string
// column) that is not excluded from the panel.
func modelSearchable(mi datasource.ModelInfo) bool {
	for _, f := range mi.Fields {
		if f.IsSearch && !f.IsExcluded {
			return true
		}
	}
	return false
}
