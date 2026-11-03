package quarkdatasource

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
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

	// fieldKeys pairs each schema column with the object key encoding/json
	// uses for its struct field, in declaration order. Quark models normally
	// carry no json tags, so entities marshal under Go-case keys ("CustomerID")
	// while the schema declares columns ("customer_id") — the key Data
	// Studio's grid reads. These pairs drive the normalization both ways
	// (entityToRecord and recordToEntity). Fields excluded from JSON
	// (json:"-") are absent.
	fieldKeys    []fieldKey
	jsonKeyByCol map[string]string // FieldInfo.Column -> JSON object key
}

type fieldKey struct {
	column  string // FieldInfo.Column (storage column)
	jsonKey string // key the struct field marshals under
}

func newStore[T any](a *Adapter, t reflect.Type, meta *quark.ModelMeta, info datasource.ModelInfo) datasource.RecordStore {
	s := &store[T]{adapter: a, typ: t, meta: meta, info: info,
		jsonKeyByCol: make(map[string]string, len(meta.Fields))}
	for _, f := range info.Fields {
		if f.IsSearch {
			s.searchCols = append(s.searchCols, f.Column)
		}
	}
	for _, f := range meta.Fields {
		key, ok := jsonFieldKey(t.Field(f.Index))
		if !ok {
			continue
		}
		s.fieldKeys = append(s.fieldKeys, fieldKey{column: f.Column, jsonKey: key})
		s.jsonKeyByCol[f.Column] = key
	}
	return s
}

// jsonFieldKey returns the object key encoding/json uses for sf, and false
// when the field is excluded from marshaling (json:"-").
func jsonFieldKey(sf reflect.StructField) (string, bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return sf.Name, true
}

// applyQuery translates the neutral Query's filters and search onto a Quark
// builder. Column names go through Quark's own SQLGuard inside the builder.
func (s *store[T]) applyQuery(ctx context.Context, qb *quark.Query[T], q datasource.Query) (*quark.Query[T], error) {
	for col, val := range q.Filters {
		v, err := s.coerceFilterValue(col, val)
		if err != nil {
			return nil, err
		}
		qb = qb.Where(col, "=", v)
	}
	// The operator filters (datasource.Query.Where). A store that ignored
	// them would answer every row while looking like it filtered, which is
	// why the contract asks first (HonoursFilterOperators) — so anything
	// this cannot express has to be an ERROR here, never a dropped clause.
	for _, f := range q.Where {
		var err error
		qb, err = s.applyOperatorFilter(qb, f)
		if err != nil {
			return nil, err
		}
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		// Nothing to look in: say so rather than answer every row, which
		// reads as "no match" while showing everything.
		if len(s.searchCols) == 0 {
			return nil, gferrors.BadRequest(fmt.Sprintf("search is not available for %s: it has no string columns to search", s.info.Name))
		}
		// The search text is data, not a pattern: a `%` or `_` typed by the
		// operator used to widen the match instead of matching itself (F13).
		// Quark's Contains escapes the text and declares the escape
		// character on the statement, so it means the same on every engine.
		parts := make([]quark.Expr, 0, len(s.searchCols))
		for _, col := range s.searchCols {
			parts = append(parts, quark.Contains(quark.Col(col), search))
		}
		// One OR-group, AND-composed with the filters above (correct precedence).
		qb = qb.WhereExpr(quark.Or(parts...))
	}
	return qb, nil
}

// applyOperatorFilter translates one neutral operator filter onto the Quark
// builder. Column names go through Quark's own SQLGuard inside the builder;
// values are bound, never interpolated.
func (s *store[T]) applyOperatorFilter(qb *quark.Query[T], f datasource.Filter) (*quark.Query[T], error) {
	switch f.Op {
	case datasource.OpEqual, datasource.OpNotEqual,
		datasource.OpGreater, datasource.OpGreaterEqual,
		datasource.OpLess, datasource.OpLessEqual:
		v, err := s.coerceFilterValue(f.Column, f.Value)
		if err != nil {
			return nil, err
		}
		return qb.Where(f.Column, sqlComparison(f.Op), v), nil

	case datasource.OpIsNull:
		// The value is the question, not the operand: isnull=false asks for
		// the rows that HAVE a value.
		// Quark's builder takes these as operators with no operand
		// (query_exec.go), which is what keeps the column quoted by its own
		// guard instead of pasted into a fragment here.
		if strings.EqualFold(strings.TrimSpace(f.Value), "false") {
			return qb.Where(f.Column, "IS NOT NULL", nil), nil
		}
		return qb.Where(f.Column, "IS NULL", nil), nil

	case datasource.OpIn, datasource.OpNotIn:
		// An empty set matches NOTHING. Dropping the clause would answer
		// every row and look like a result — the contract says so, and it
		// is the one place a filter can silently become no filter.
		values := make([]any, 0, len(f.Values))
		for _, raw := range f.Values {
			v, err := s.coerceFilterValue(f.Column, raw)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		if len(values) == 0 {
			// A comparison no row satisfies, expressed with a bound value
			// rather than a literal fragment.
			return qb.WhereExpr(quark.Cmp(quark.Lit(1), "=", quark.Lit(0))), nil
		}
		exprs := make([]quark.Expr, 0, len(values))
		for _, v := range values {
			exprs = append(exprs, quark.Lit(v))
		}
		if f.Op == datasource.OpNotIn {
			return qb.WhereExpr(quark.NotIn(quark.Col(f.Column), exprs...)), nil
		}
		return qb.WhereExpr(quark.In(quark.Col(f.Column), exprs...)), nil

	case datasource.OpContains, datasource.OpStartsWith, datasource.OpEndsWith:
		return s.applyPatternFilter(qb, f)
	}
	// An operator this store cannot express is refused by name. Falling
	// back to equality would answer a different question than the one asked.
	return nil, gferrors.BadRequest(fmt.Sprintf("filter operator %q is not supported by this data source", f.Op))
}

// applyPatternFilter builds the LIKE for contains/startswith/endswith.
//
// The contract requires the value to match LITERALLY. Quark's escaped forms
// (Contains, StartsWith, EndsWith) escape the text and declare the escape
// character on the statement, so a `%` or `_` in the value matches itself on
// every engine — including SQLite and Oracle, whose LIKE has no default
// escape and where this store used to refuse such a value rather than
// widen the match (QK-25, closed from this side). The plain
// Cmp(col, "LIKE", …) form is the one Quark changes at its 2.0 (its
// DEP-2026-009); the escaped forms do not change.
func (s *store[T]) applyPatternFilter(qb *quark.Query[T], f datasource.Filter) (*quark.Query[T], error) {
	col := quark.Col(f.Column)
	switch f.Op {
	case datasource.OpStartsWith:
		return qb.WhereExpr(quark.StartsWith(col, f.Value)), nil
	case datasource.OpEndsWith:
		return qb.WhereExpr(quark.EndsWith(col, f.Value)), nil
	}
	return qb.WhereExpr(quark.Contains(col, f.Value)), nil
}

// sqlComparison maps a comparison operator to its SQL spelling.
func sqlComparison(op datasource.FilterOp) string {
	switch op {
	case datasource.OpNotEqual:
		return "!="
	case datasource.OpGreater:
		return ">"
	case datasource.OpGreaterEqual:
		return ">="
	case datasource.OpLess:
		return "<"
	case datasource.OpLessEqual:
		return "<="
	}
	return "="
}

// HonoursFilterOperators reports that this store applies Query.Where
// (datasource.OperatorFilterSource). Before it did, the panel refused every
// operator filter over a Quark-backed model rather than answer it unfiltered
// — which was the right refusal, and this is the answer.
func (s *store[T]) HonoursFilterOperators() bool { return true }

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

	counted, err := s.applyQuery(ctx, quark.For[T](ctx, s.adapter.provider), q)
	if err != nil {
		return datasource.Page{}, err
	}
	total, err := counted.Count()
	if err != nil {
		return datasource.Page{}, fmt.Errorf("quarkdatasource: count %s: %w", s.info.Name, err)
	}

	qb, err := s.applyQuery(ctx, quark.For[T](ctx, s.adapter.provider), q)
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
		rec, err := s.entityToRecord(&items[i])
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
// A row that does not exist is the contract's not-found (404), the answer
// the record endpoints give a row out of scope: Quark's ErrNotFound used to
// reach the panel unmapped, a 500 that named nothing.
func (s *store[T]) Get(ctx context.Context, id string) (datasource.Record, error) {
	pk, err := s.parseID(id)
	if err != nil {
		return nil, err
	}
	entity, err := quark.For[T](ctx, s.adapter.provider).Find(pk)
	if err != nil {
		return nil, s.classify(err, id, nil)
	}
	return s.entityToRecord(&entity)
}

// Create inserts a record and returns the created row (with the
// database-assigned id, which Quark writes back onto the entity). The record
// is read field by field and refused whole, with a 422 naming every
// offending key, before anything is written (see readRecord); a write the
// database refuses on a unique key is a 409.
func (s *store[T]) Create(ctx context.Context, rec datasource.Record) (datasource.Record, error) {
	if s.info.ReadOnly {
		return nil, s.readOnlyErr()
	}
	entity, written, err := s.recordToEntity(rec)
	if err != nil {
		return nil, err
	}
	if err := quark.For[T](ctx, s.adapter.provider).Create(entity); err != nil {
		return nil, s.classify(err, "", written)
	}
	return s.entityToRecord(entity)
}

// Update applies a partial change set by PK, via Quark's UpdateMap (which can
// write zero values — unlike a full-entity save). A record naming nothing the
// operator may write is a 400, as on the Nucleus adapter; an update that
// touches no row is a not-found rather than a success, so the panel neither
// answers {"updated": true} nor records a change that did not happen.
func (s *store[T]) Update(ctx context.Context, id string, rec datasource.Record) error {
	if s.info.ReadOnly {
		return s.readOnlyErr()
	}
	pk, err := s.parseID(id)
	if err != nil {
		return err
	}
	updates, written, err := s.recordToColumnMap(rec)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return gferrors.BadRequest("no updatable fields provided")
	}
	rows, err := quark.For[T](ctx, s.adapter.provider).
		Where(s.meta.PK.Column, "=", pk).
		UpdateMap(updates)
	if err != nil {
		return s.classify(err, id, written)
	}
	if rows == 0 {
		// Zero rows affected is not proof of absence everywhere: MySQL
		// counts the rows an UPDATE changed, not the ones it matched, so a
		// write of the values the row already holds answers 0. Ask.
		exists, err := quark.For[T](ctx, s.adapter.provider).Where(s.meta.PK.Column, "=", pk).Count()
		if err != nil {
			return s.classify(err, id, nil)
		}
		if exists == 0 {
			return gferrors.NotFound(s.info.Name, id)
		}
	}
	return nil
}

// Delete removes one record by id, honoring Quark's semantics: soft delete
// when the model has a deleted_at column, hard delete otherwise. A delete
// that removes nothing — the row does not exist, or was already
// soft-deleted — is a not-found rather than a success, so a batch counts it
// as the failure it is.
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
	rows, err := quark.For[T](ctx, s.adapter.provider).Delete(entity)
	if err != nil {
		var de *gferrors.DomainError
		if !errors.As(err, &de) && !quark.IsUniqueViolation(err) && errors.Is(err, quark.ErrConstraintViolation) {
			// What refuses a delete is, in practice, a foreign key: another
			// row still points at this one.
			return gferrors.Conflict(fmt.Sprintf("%s '%s' cannot be deleted: a constraint of the database protects it (another record may still reference it)", s.info.Name, id))
		}
		return s.classify(err, id, nil)
	}
	if rows == 0 {
		return gferrors.NotFound(s.info.Name, id)
	}
	return nil
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

var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// parseID narrows the boundary string id (D1) to the PK field's Go type:
// integers and strings by kind, and any other key type with a textual form
// — google/uuid's UUID ([16]byte), ULIDs — through encoding.TextUnmarshaler,
// which is also what database/sql needs for the value to round-trip. An id
// that does not narrow is the caller's mistake and surfaces as a 400 (a
// plain error used to reach the operator as a 500).
func (s *store[T]) parseID(id string) (any, error) {
	if s.info.ReadOnly {
		return nil, s.readOnlyErr()
	}
	id = strings.TrimSpace(id)
	switch s.meta.PK.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, s.invalidIDErr(id, "ids are integers")
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, s.invalidIDErr(id, "ids are non-negative integers")
		}
		return n, nil
	case reflect.String:
		if id == "" {
			return nil, s.invalidIDErr(id, "ids must not be empty")
		}
		return id, nil
	}
	if typ := s.typ.Field(s.meta.PK.Index).Type; reflect.PointerTo(typ).Implements(textUnmarshalerType) {
		v := reflect.New(typ)
		if err := v.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(id)); err != nil {
			return nil, s.invalidIDErr(id, err.Error())
		}
		return v.Elem().Interface(), nil
	}
	return nil, fmt.Errorf("quarkdatasource: unsupported primary key kind %s for %s", s.meta.PK.Kind, s.info.Name)
}

func (s *store[T]) invalidIDErr(id, why string) error {
	return gferrors.BadRequest(fmt.Sprintf("invalid id %q for %s: %s", id, s.info.Name, why))
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

// entityToRecord round-trips an entity through JSON and then re-keys every
// schema field to its storage column (FieldInfo.Column) — the key Data
// Studio's grid reads cells by. Without this, Quark models (which normally
// have no json tags) marshal Go-case keys and every cell renders empty
// (PR-DS-01). Keys outside the schema (relations, extra JSON) pass through
// unchanged, preserving the round-trip fidelity ADR-001 O3 asks for.
func (s *store[T]) entityToRecord(entity any) (datasource.Record, error) {
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("quarkdatasource: marshal entity: %w", err)
	}
	var rec datasource.Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("quarkdatasource: unmarshal entity: %w", err)
	}
	for _, fk := range s.fieldKeys {
		if fk.jsonKey == fk.column {
			continue
		}
		if v, ok := rec[fk.jsonKey]; ok {
			delete(rec, fk.jsonKey)
			rec[fk.column] = v
		}
	}
	return rec, nil
}

// recordToEntity builds a *T from a Record: every key is resolved to a schema
// field (by column, Go name or the struct's JSON key) and its value coerced
// to the field's type and set on the entity by its index — so a multi-word
// column ("customer_id") reaches its Go field (CustomerID), and a field
// excluded from JSON (json:"-") is stored like any other: the tenant a
// scoped create stamps under its column key used to be dropped, which stored
// the row under no tenant. The PK and read-only (version) fields are left to
// the database.
//
// A record the store cannot write faithfully is refused whole with one 422
// naming every offending key (readRecord): a key that names no field (it
// used to be dropped in silence), a value of the wrong type (it used to be a
// 500 carrying the decoder's text), a field named twice — under its column
// and its JSON key, or two letter cases — which used to be resolved by map
// order, so a caller that stamps one key (the panel's tenant) could be
// outvoted by another it does not know, and a required field left out or
// empty.
//
// It returns the fields it set, for classify to name a unique one.
func (s *store[T]) recordToEntity(rec datasource.Record) (*T, []fieldValue, error) {
	values, problems := s.readRecord(rec, false)
	if err := validationErr(problems); err != nil {
		return nil, nil, err
	}
	entity := new(T)
	ev := reflect.ValueOf(entity).Elem()
	for _, fv := range values {
		if fv.null {
			continue
		}
		ev.Field(fv.index).Set(fv.value)
	}
	return entity, values, nil
}

// fieldForInput resolves a record key to its schema field: by column or Go
// name (ModelInfo.Field), or by the JSON key the struct field marshals under.
func (s *store[T]) fieldForInput(key string) (datasource.FieldInfo, bool) {
	if fi, ok := s.info.Field(key); ok {
		return fi, true
	}
	for _, fk := range s.fieldKeys {
		if strings.EqualFold(key, fk.jsonKey) {
			return s.info.Field(fk.column)
		}
	}
	return datasource.FieldInfo{}, false
}

// recordToColumnMap resolves record keys (column or Go field name) to column
// names and coerces their values to the column's Go type, for UpdateMap. A
// JSON-key alias is refused here rather than honoured, on purpose: the
// panel's tenant guard knows a Quark field by column and Go name only, so an
// update that honoured a key the guard cannot see could move a row (it used
// to be ignored in silence, which answered "updated" for a change that was
// not made). The checks are readRecord's, so an update can no more empty a
// required field or store a value of the wrong type than a create can.
func (s *store[T]) recordToColumnMap(rec datasource.Record) (map[string]any, []fieldValue, error) {
	values, problems := s.readRecord(rec, true)
	if err := validationErr(problems); err != nil {
		return nil, nil, err
	}
	out := make(map[string]any, len(values))
	for _, fv := range values {
		out[fv.column] = bindValue(fv)
	}
	return out, values, nil
}

// classify turns what Quark answered a read or a write with into the answer
// the panel gives a client: a missing row is a 404, a value a unique key
// already holds a 409 naming the field when one can be named, another
// constraint the row breaks a 409 too. The message is the adapter's, never
// the engine's: the driver's text ("UNIQUE constraint failed: …",
// "SQLSTATE 23505") is what reached the operator as a 500 before, and it
// says more about the schema than a client of the panel needs.
//
// A unique violation is recognised by quark.IsUniqueViolation, the predicate
// of the client that ran the statement — it classifies on the driver's error
// code for every engine whose Quark driver module is linked, which an
// application on Quark links to open its database at all. An engine it does
// not classify still arrives as quark.ErrConstraintViolation and is answered
// as a constraint, so a duplicate never falls through to a 500.
//
// An error that is already a domain error (a model hook refusing the write,
// a tenant router's refusal) passes through unchanged, and so does every
// other failure: an unreachable database is the server's problem, and a 500
// says so.
func (s *store[T]) classify(err error, id string, written []fieldValue) error {
	var de *gferrors.DomainError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &de):
		return err
	case errors.Is(err, quark.ErrNotFound):
		return gferrors.NotFound(s.info.Name, id)
	case quark.IsUniqueViolation(err):
		return s.uniqueConflict(written)
	case errors.Is(err, quark.ErrConstraintViolation):
		return gferrors.Conflict(fmt.Sprintf("%s: the record breaks a constraint of the database (a unique key, a foreign key, a NOT NULL or a CHECK)", s.info.Name))
	}
	return err
}

// uniqueConflict is the 409 of a unique violation. When exactly one unique
// field was written it is named, with its key in the details the way a 422
// names a field; otherwise the message says which kind of conflict it is
// without guessing a field.
func (s *store[T]) uniqueConflict(written []fieldValue) error {
	var keys []string
	for _, fv := range written {
		if fv.unique && !fv.null {
			keys = append(keys, fv.key)
		}
	}
	if len(keys) == 1 {
		return gferrors.Conflict(fmt.Sprintf("%s: a record with this %s already exists", s.info.Name, keys[0])).
			WithDetails(map[string]string{keys[0]: "already exists"})
	}
	return gferrors.Conflict(fmt.Sprintf("%s: a record with the same unique value already exists", s.info.Name))
}

// assignPK writes a parsed id (int64/uint64/string from parseID, or a value
// of the PK's own type narrowed through TextUnmarshaler) into the PK struct
// field.
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
		rv := reflect.ValueOf(pk)
		if !rv.IsValid() || !rv.Type().AssignableTo(field.Type()) {
			return fmt.Errorf("quarkdatasource: unsupported pk value %T", pk)
		}
		field.Set(rv)
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
