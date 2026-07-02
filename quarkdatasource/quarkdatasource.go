// Package quarkdatasource implements Orbit's datasource contract (orbit
// ADR-001) over a Quark ORM client, so Data Studio browses and edits
// Quark-managed models (quantum QADR-0006, Caso 2).
//
// It is the second implementation of the contract — the one that validates the
// abstraction did not keep Nucleus's shape. The catalogue comes from the model
// structs' Quark tags (db/pk/quark), not from table introspection, so Data
// Studio sees the same Go-level metadata Quark itself uses.
//
// # Registration is generic, per model
//
// Quark's query API is typed (quark.For[T]; its ADR-0002/0014 design), so a
// model's CRUD operations cannot be bound from a reflect.Type at runtime. Each
// model is registered with a generic call, which monomorphizes the typed query
// path once at wiring time:
//
//	ds := quarkdatasource.New(client)
//	quarkdatasource.Register[User](ds)
//	quarkdatasource.Register[Post](ds)
//
//	app := nucleus.New().
//	    Mount(orbit.Module(orbit.Config{Prefix: "/admin", DataSource: ds})).
//	    Build()
//
// New accepts any quark.ClientProvider: a *quark.Client, or a
// *quark.TenantRouter so every Data Studio query runs under Quark's own tenant
// scoping (WHERE-injection or native RLS, per the router's strategy).
//
// # Semantics
//
//   - IDs are strings at the boundary (ADR-001 D1) and are narrowed to the PK
//     field's Go kind. Models with a composite primary key are listed read-only:
//     List/Count work, Get/Create/Update/Delete return an error.
//   - Records are the model's JSON object (ADR-001 D2): the adapter round-trips
//     entities through encoding/json, so what Data Studio shows is exactly what
//     the struct marshals to (including quark.Nullable values).
//   - Delete follows Quark's semantics: soft delete when the model has a
//     deleted_at column, hard delete otherwise.
//   - Store's dbAlias is ignored: a Quark client is bound to one database. Use
//     one adapter per client if you browse several.
package quarkdatasource

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
)

// Option configures the adapter.
type Option func(*Adapter)

// WithTenantColumn names the column that scopes models to a tenant. Models
// carrying it get ModelInfo.TenantField set, so Data Studio's tenant filter
// applies. This complements — it does not replace — passing a
// *quark.TenantRouter as the provider, which enforces scoping in Quark itself.
func WithTenantColumn(column string) Option {
	return func(a *Adapter) { a.tenantColumn = strings.TrimSpace(column) }
}

// Adapter implements datasource.DataSource over a Quark client. Populate it
// with Register[T] for each model; registration order is preserved in All.
type Adapter struct {
	provider     quark.ClientProvider
	tenantColumn string

	mu     sync.RWMutex
	names  []string
	models map[string]*registeredModel
}

type registeredModel struct {
	info  datasource.ModelInfo
	store datasource.RecordStore
}

// New returns an empty adapter bound to provider (a *quark.Client or a
// *quark.TenantRouter). Register models with Register[T].
func New(provider quark.ClientProvider, opts ...Option) *Adapter {
	a := &Adapter{provider: provider, models: make(map[string]*registeredModel)}
	for _, o := range opts {
		o(a)
	}
	return a
}

var _ datasource.DataSource = (*Adapter)(nil)

// All returns the registered models in registration order.
func (a *Adapter) All() []datasource.ModelInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]datasource.ModelInfo, 0, len(a.names))
	for _, n := range a.names {
		out = append(out, a.models[n].info)
	}
	return out
}

// Get returns one model by name.
func (a *Adapter) Get(name string) (datasource.ModelInfo, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	m, ok := a.models[name]
	if !ok {
		return datasource.ModelInfo{}, false
	}
	return m.info, true
}

// Store returns the RecordStore for a model. dbAlias is accepted for contract
// compatibility and ignored: the Quark client is bound to one database.
func (a *Adapter) Store(modelName, _ string) (datasource.RecordStore, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	m, ok := a.models[modelName]
	if !ok {
		return nil, fmt.Errorf("quarkdatasource: unknown model %q", modelName)
	}
	return m.store, nil
}

// Register adds model T to the adapter's catalogue and builds its typed record
// store. T must be a struct with Quark tags. Registering the same name twice
// replaces the previous entry.
func Register[T any](a *Adapter) error {
	t := reflect.TypeOf((*T)(nil)).Elem()
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("quarkdatasource: Register[%s]: model must be a struct", t)
	}
	meta := quark.GetModelMetaByType(t)
	if meta == nil {
		return fmt.Errorf("quarkdatasource: Register[%s]: no Quark metadata (missing db tags?)", t)
	}

	info := a.buildModelInfo(t, meta)
	st := newStore[T](a, t, meta, info)

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.models[info.Name]; !exists {
		a.names = append(a.names, info.Name)
	}
	a.models[info.Name] = &registeredModel{info: info, store: st}
	return nil
}

// buildModelInfo maps Quark model metadata to the neutral ModelInfo. Quark tags
// carry storage-level facts (column, pk, not_null, unique); presentation
// metadata Quark deliberately does not have (labels, HTML types, list/filter
// flags) is derived from the Go type with permissive defaults.
func (a *Adapter) buildModelInfo(t reflect.Type, meta *quark.ModelMeta) datasource.ModelInfo {
	readOnly := meta.HasCompositePK || !meta.HasPK

	fields := make([]datasource.FieldInfo, 0, len(meta.Fields))
	fkCols := belongsToColumns(meta)
	tenantField := ""
	for _, f := range meta.Fields {
		goName := t.Field(f.Index).Name
		isTenant := a.tenantColumn != "" && strings.EqualFold(f.Column, a.tenantColumn)
		if isTenant {
			tenantField = f.Column
		}
		fields = append(fields, datasource.FieldInfo{
			Name:          goName,
			Column:        f.Column,
			Label:         humanize(goName),
			GoType:        goTypeName(f.Type),
			HTMLType:      htmlType(f.Type),
			IsPK:          f.IsPK,
			IsRequired:    f.NotNull,
			IsReadOnly:    f.IsPK || f.IsVersion,
			IsList:        true,
			IsSearch:      f.Kind == reflect.String,
			IsFilter:      filterableKind(f.Kind),
			IsForeignKey:  fkCols[strings.ToLower(f.Column)] != "",
			IsTenantField: isTenant,
			ForeignModel:  fkCols[strings.ToLower(f.Column)],
		})
	}

	fks := make([]datasource.ForeignKey, 0)
	relNames := make([]string, 0, len(meta.Relations))
	for name := range meta.Relations {
		relNames = append(relNames, name)
	}
	sort.Strings(relNames)
	for _, name := range relNames {
		rel := meta.Relations[name]
		if rel.Type != "belongs_to" || rel.RefType == nil {
			continue
		}
		refMeta := quark.GetModelMetaByType(rel.RefType)
		refTable := ""
		if refMeta != nil {
			refTable = refMeta.Table
		}
		fks = append(fks, datasource.ForeignKey{
			FieldName:     rel.Field,
			Column:        rel.JoinCol,
			ForeignModel:  rel.RefType.Name(),
			ForeignTable:  refTable,
			ForeignColumn: "id",
		})
	}

	var idxs []datasource.Index
	for _, f := range meta.Fields {
		if f.Unique {
			idxs = append(idxs, datasource.Index{Columns: []string{f.Column}, Unique: true})
		}
	}

	pkName := ""
	if meta.HasPK && !meta.HasCompositePK {
		pkName = t.Field(meta.PK.Index).Name
	}

	return datasource.ModelInfo{
		Name:        t.Name(),
		Plural:      humanize(t.Name()) + "s",
		Table:       meta.Table,
		PrimaryKey:  pkName,
		ReadOnly:    readOnly,
		TenantField: tenantField,
		Fields:      fields,
		ForeignKeys: fks,
		Indexes:     idxs,
	}
}

// belongsToColumns maps lower-cased FK column -> related model name for the
// model's belongs_to relations.
func belongsToColumns(meta *quark.ModelMeta) map[string]string {
	out := make(map[string]string)
	for _, rel := range meta.Relations {
		if rel.Type == "belongs_to" && rel.JoinCol != "" && rel.RefType != nil {
			out[strings.ToLower(rel.JoinCol)] = rel.RefType.Name()
		}
	}
	return out
}

// humanize turns a Go identifier into a label: "CreatedAt" -> "Created At".
func humanize(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(name[i-1])
			if prev < 'A' || prev > 'Z' {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// goTypeName renders a field's Go type compactly: basic kinds by kind name
// (so the panel's bool-filter normalization keys on "bool"), others by type.
func goTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return t.Kind().String()
	default:
		return t.String()
	}
}

func htmlType(t reflect.Type) string {
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return "datetime-local"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "checkbox"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "text"
	}
}

func filterableKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
