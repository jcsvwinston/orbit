package quarkdatasource

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
)

// defaultPageSize matches the framework-side Data Studio default.
const defaultPageSize = 25

// store implements datasource.RecordStore for one registered model. It is
// built inside the generic Register[T], which is where Quark's typed query
// path (quark.For[T]) gets bound; everything model-specific lives in the
// captured type parameter and metadata.
type store[T any] struct {
	adapter *Adapter
	typ     reflect.Type
	meta    *quark.ModelMeta
	info    datasource.ModelInfo

	searchCols []string // columns LIKE-matched by Query.Search
}

func newStore[T any](a *Adapter, t reflect.Type, meta *quark.ModelMeta, info datasource.ModelInfo) datasource.RecordStore {
	s := &store[T]{adapter: a, typ: t, meta: meta, info: info}
	for _, f := range info.Fields {
		if f.IsSearch {
			s.searchCols = append(s.searchCols, f.Column)
		}
	}
	return s
}

// applyQuery translates the neutral Query's filters and search onto a Quark
// builder. Column names go through Quark's own SQLGuard inside the builder.
func (s *store[T]) applyQuery(qb *quark.Query[T], q datasource.Query) (*quark.Query[T], error) {
	for col, val := range q.Filters {
		v, err := s.coerceFilterValue(col, val)
		if err != nil {
			return nil, err
		}
		qb = qb.Where(col, "=", v)
	}
	if search := strings.TrimSpace(q.Search); search != "" && len(s.searchCols) > 0 {
		pattern := "%" + search + "%"
		parts := make([]quark.Expr, 0, len(s.searchCols))
		for _, col := range s.searchCols {
			parts = append(parts, quark.Cmp(quark.Col(col), "LIKE", quark.Lit(pattern)))
		}
		// One OR-group, AND-composed with the filters above (correct precedence).
		qb = qb.WhereExpr(quark.Or(parts...))
	}
	return qb, nil
}

// List runs a paginated query. Total is a real count over the same
// filters (never an estimate), so IsEstimated is always false.
func (s *store[T]) List(ctx context.Context, q datasource.Query) (datasource.Page, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = defaultPageSize
	}

	counted, err := s.applyQuery(quark.For[T](ctx, s.adapter.provider), q)
	if err != nil {
		return datasource.Page{}, err
	}
	total, err := counted.Count()
	if err != nil {
		return datasource.Page{}, fmt.Errorf("quarkdatasource: count %s: %w", s.info.Name, err)
	}

	qb, err := s.applyQuery(quark.For[T](ctx, s.adapter.provider), q)
	if err != nil {
		return datasource.Page{}, err
	}
	for _, clause := range parseOrderBy(q.OrderBy) {
		qb = qb.OrderBy(clause.column, clause.direction)
	}
	items, err := qb.Limit(size).Offset((page - 1) * size).List()
	if err != nil {
		return datasource.Page{}, fmt.Errorf("quarkdatasource: list %s: %w", s.info.Name, err)
	}

	records := make([]datasource.Record, 0, len(items))
	for i := range items {
		rec, err := entityToRecord(&items[i])
		if err != nil {
			return datasource.Page{}, err
		}
		records = append(records, rec)
	}

	totalPages := 0
	if size > 0 {
		totalPages = int((total + int64(size) - 1) / int64(size))
	}
	return datasource.Page{
		Items:       records,
		Total:       total,
		Page:        page,
		PageSize:    size,
		TotalPages:  totalPages,
		IsEstimated: false,
		HasMore:     int64(page)*int64(size) < total,
	}, nil
}

// Get fetches one record by its string id, narrowed to the PK's Go kind (D1).
func (s *store[T]) Get(ctx context.Context, id string) (datasource.Record, error) {
	pk, err := s.parseID(id)
	if err != nil {
		return nil, err
	}
	entity, err := quark.For[T](ctx, s.adapter.provider).Find(pk)
	if err != nil {
		return nil, err
	}
	return entityToRecord(&entity)
}

// Create inserts a record and returns the created row (with the
// database-assigned id, which Quark writes back onto the entity).
func (s *store[T]) Create(ctx context.Context, rec datasource.Record) (datasource.Record, error) {
	if s.info.ReadOnly {
		return nil, s.readOnlyErr()
	}
	entity, err := s.recordToEntity(rec)
	if err != nil {
		return nil, err
	}
	if err := quark.For[T](ctx, s.adapter.provider).Create(entity); err != nil {
		return nil, err
	}
	return entityToRecord(entity)
}

// Update applies a partial change set by PK, via Quark's UpdateMap (which can
// write zero values — unlike a full-entity save).
func (s *store[T]) Update(ctx context.Context, id string, rec datasource.Record) error {
	if s.info.ReadOnly {
		return s.readOnlyErr()
	}
	pk, err := s.parseID(id)
	if err != nil {
		return err
	}
	updates, err := s.recordToColumnMap(rec)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	_, err = quark.For[T](ctx, s.adapter.provider).
		Where(s.meta.PK.Column, "=", pk).
		UpdateMap(updates)
	return err
}

// Delete removes one record by id, honoring Quark's semantics: soft delete
// when the model has a deleted_at column, hard delete otherwise.
func (s *store[T]) Delete(ctx context.Context, id string) error {
	if s.info.ReadOnly {
		return s.readOnlyErr()
	}
	pk, err := s.parseID(id)
	if err != nil {
		return err
	}
	entity := new(T)
	pkField := reflect.ValueOf(entity).Elem().Field(s.meta.PK.Index)
	if err := assignPK(pkField, pk); err != nil {
		return err
	}
	_, err = quark.For[T](ctx, s.adapter.provider).Delete(entity)
	return err
}

// Count returns the real row count. Present is false when the backing table
// does not exist yet (e.g. migrations not run).
func (s *store[T]) Count(ctx context.Context) (datasource.CountResult, error) {
	total, err := quark.For[T](ctx, s.adapter.provider).Count()
	if err != nil {
		if isTableMissingErr(err) {
			return datasource.CountResult{Present: false}, nil
		}
		return datasource.CountResult{}, fmt.Errorf("quarkdatasource: count %s: %w", s.info.Name, err)
	}
	return datasource.CountResult{Count: total, IsEstimated: false, Present: true}, nil
}

// TableExists probes the table with a LIMIT 1 query.
func (s *store[T]) TableExists(ctx context.Context) bool {
	_, err := quark.For[T](ctx, s.adapter.provider).Limit(1).List()
	return err == nil
}

// ---- helpers ----

func (s *store[T]) readOnlyErr() error {
	if s.meta.HasCompositePK {
		return fmt.Errorf("quarkdatasource: %s has a composite primary key; Data Studio edits require a single key (model is read-only)", s.info.Name)
	}
	return fmt.Errorf("quarkdatasource: %s has no primary key (model is read-only)", s.info.Name)
}

// parseID narrows the boundary string id (D1) to the PK field's Go kind.
func (s *store[T]) parseID(id string) (any, error) {
	if s.info.ReadOnly {
		return nil, s.readOnlyErr()
	}
	id = strings.TrimSpace(id)
	switch s.meta.PK.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid id %q for %s", id, s.info.Name)
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid id %q for %s", id, s.info.Name)
		}
		return n, nil
	case reflect.String:
		if id == "" {
			return nil, fmt.Errorf("quarkdatasource: empty id for %s", s.info.Name)
		}
		return id, nil
	default:
		return nil, fmt.Errorf("quarkdatasource: unsupported primary key kind %s for %s", s.meta.PK.Kind, s.info.Name)
	}
}

// coerceFilterValue converts a panel filter value (always a string) to the
// column's Go kind, so typed engines compare correctly.
func (s *store[T]) coerceFilterValue(column, value string) (any, error) {
	fm, ok := s.meta.FieldByCol[strings.ToLower(column)]
	if !ok {
		return value, nil // unknown column: let Quark's guard/SQL surface the error
	}
	switch fm.Kind {
	case reflect.Bool:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off":
			return false, nil
		default:
			return nil, fmt.Errorf("quarkdatasource: invalid boolean %q for filter %q", value, column)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid number %q for filter %q", value, column)
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid number %q for filter %q", value, column)
		}
		return n, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid number %q for filter %q", value, column)
		}
		return f, nil
	default:
		return value, nil
	}
}

// entityToRecord round-trips an entity through JSON, so records carry exactly
// the JSON object the struct marshals to (ADR-001 O3 fidelity: the SPA sees
// the same shape a JSON API over these models would emit).
func entityToRecord(entity any) (datasource.Record, error) {
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("quarkdatasource: marshal entity: %w", err)
	}
	var rec datasource.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("quarkdatasource: unmarshal entity: %w", err)
	}
	return rec, nil
}

// recordToEntity builds a *T from a Record via the inverse JSON round-trip,
// after dropping PK and read-only (version) fields — the database owns those.
func (s *store[T]) recordToEntity(rec datasource.Record) (*T, error) {
	clean := make(map[string]any, len(rec))
	for k, v := range rec {
		if fi, ok := s.info.Field(k); ok && (fi.IsPK || fi.IsReadOnly) {
			continue
		}
		clean[k] = v
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return nil, fmt.Errorf("quarkdatasource: marshal record: %w", err)
	}
	entity := new(T)
	if err := json.Unmarshal(data, entity); err != nil {
		return nil, fmt.Errorf("quarkdatasource: invalid record for %s: %w", s.info.Name, err)
	}
	return entity, nil
}

// recordToColumnMap resolves record keys (column or Go field name) to column
// names and coerces JSON values to the column's Go type, for UpdateMap.
func (s *store[T]) recordToColumnMap(rec datasource.Record) (map[string]any, error) {
	out := make(map[string]any, len(rec))
	for k, v := range rec {
		fi, ok := s.info.Field(k)
		if !ok || fi.IsPK || fi.IsReadOnly {
			continue
		}
		fm, ok := s.meta.FieldByCol[strings.ToLower(fi.Column)]
		if !ok {
			continue
		}
		cv, err := coerceJSONValue(v, fm.Kind)
		if err != nil {
			return nil, fmt.Errorf("quarkdatasource: invalid value for %s: %w", k, err)
		}
		out[fi.Column] = cv
	}
	return out, nil
}

// coerceJSONValue converts a record value to the column's Go kind for binding.
// Values usually arrive JSON-decoded (float64/bool/string/nil), but Record is
// map[string]any, so native Go numerics are accepted too.
func coerceJSONValue(v any, kind reflect.Kind) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch kind {
	case reflect.Bool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("want bool, got %T", v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, ok := toInt64(v); ok {
			return n, nil
		}
		if s, ok := v.(string); ok {
			return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		}
		return nil, fmt.Errorf("want number, got %T", v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, ok := toInt64(v); ok {
			if n < 0 {
				return nil, fmt.Errorf("negative value for unsigned column")
			}
			return uint64(n), nil
		}
		if s, ok := v.(string); ok {
			return strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		}
		return nil, fmt.Errorf("want number, got %T", v)
	case reflect.Float32, reflect.Float64:
		switch n := v.(type) {
		case float64:
			return n, nil
		case float32:
			return float64(n), nil
		case string:
			return strconv.ParseFloat(strings.TrimSpace(n), 64)
		}
		if n, ok := toInt64(v); ok {
			return float64(n), nil
		}
		return nil, fmt.Errorf("want number, got %T", v)
	default:
		return v, nil
	}
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

// assignPK writes a parsed id (int64/uint64/string from parseID) into the PK
// struct field.
func assignPK(field reflect.Value, pk any) error {
	switch v := pk.(type) {
	case int64:
		if !field.CanInt() {
			return fmt.Errorf("quarkdatasource: pk field is not an integer")
		}
		field.SetInt(v)
	case uint64:
		if !field.CanUint() {
			return fmt.Errorf("quarkdatasource: pk field is not an unsigned integer")
		}
		field.SetUint(v)
	case string:
		if field.Kind() != reflect.String {
			return fmt.Errorf("quarkdatasource: pk field is not a string")
		}
		field.SetString(v)
	default:
		return fmt.Errorf("quarkdatasource: unsupported pk value %T", pk)
	}
	return nil
}

type orderClause struct {
	column    string
	direction string
}

// parseOrderBy splits a validated "col dir[, col dir]" clause (the panel
// validates it against ModelInfo before it gets here) into Quark OrderBy args.
func parseOrderBy(raw string) []orderClause {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]orderClause, 0, len(parts))
	for _, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) == 0 {
			continue
		}
		dir := "ASC"
		if len(fields) > 1 && strings.EqualFold(fields[1], "desc") {
			dir = "DESC"
		}
		out = append(out, orderClause{column: fields[0], direction: dir})
	}
	return out
}

// isTableMissingErr matches the "relation/table does not exist" error shapes of
// the engines Quark supports.
func isTableMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "undefined table")
}
