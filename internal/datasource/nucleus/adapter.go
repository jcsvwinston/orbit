// Package nucleus is the Nucleus-backed implementation of Orbit's neutral
// datasource contract (ADR-001). It is the single place in Orbit that imports
// nucleus/pkg/model and pkg/db: it wraps a *model.Registry, per-alias *db.DB
// handles, and model.NewCRUD, and translates them to datasource.ModelInfo /
// datasource.RecordStore / datasource.Page. The Data Studio panel depends only
// on the datasource contract; a Quark adapter will implement the same contract
// later, proving the abstraction did not keep Nucleus's shape.
package nucleus

import (
	"fmt"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/signals"

	"github.com/jcsvwinston/orbit/datasource"
)

// Config wires the adapter from the same runtime accessors the panel used to
// hold directly (registry, per-alias handles, the signals bus).
type Config struct {
	// Registry is the application's shared model registry (rt.Models()).
	Registry *model.Registry

	// DefaultAlias is the database alias used when a model declares none and a
	// request specifies none.
	DefaultAlias string

	// Resolve maps a database alias to its engine-aware handle and dialect
	// string. An empty alias means DefaultAlias. It replaces the panel's old
	// databaseHandle + databaseRuntimeInfoByAlias plumbing.
	Resolve func(alias string) (handle *db.DB, dialect string, err error)

	// Bus is passed to model.NewCRUD (may be nil).
	Bus *signals.Bus

	// Observer, when non-nil, is installed on each CRUD as the fallback live-SQL
	// feed unless BusConnected reports true — mirroring the panel's getCRUD,
	// which skipped the per-CRUD observer once the observability bus was wired.
	Observer     model.SQLQueryObserver
	BusConnected func() bool
}

// Adapter implements datasource.DataSource over Nucleus.
type Adapter struct {
	cfg Config

	mu     sync.Mutex
	stores map[string]*store // keyed by alias + "::" + model name
}

// New returns a Nucleus-backed DataSource. Registry and Resolve are required.
func New(cfg Config) *Adapter {
	return &Adapter{cfg: cfg, stores: make(map[string]*store)}
}

var _ datasource.DataSource = (*Adapter)(nil)

// All returns every registered model as neutral ModelInfo.
func (a *Adapter) All() []datasource.ModelInfo {
	metas := a.cfg.Registry.All()
	out := make([]datasource.ModelInfo, 0, len(metas))
	for _, m := range metas {
		out = append(out, toModelInfo(m))
	}
	return out
}

// Get returns one model by name.
func (a *Adapter) Get(name string) (datasource.ModelInfo, bool) {
	m, ok := a.cfg.Registry.Get(name)
	if !ok {
		return datasource.ModelInfo{}, false
	}
	return toModelInfo(m), true
}

// Store resolves the RecordStore for a (model, dbAlias). An empty dbAlias falls
// back to the model's declared alias, then to DefaultAlias — the same order the
// panel's handlers applied. Stores are cached per (alias, model), like getCRUD.
func (a *Adapter) Store(modelName, dbAlias string) (datasource.RecordStore, error) {
	meta, ok := a.cfg.Registry.Get(modelName)
	if !ok {
		return nil, fmt.Errorf("datasource/nucleus: unknown model %q", modelName)
	}

	alias := strings.TrimSpace(dbAlias)
	if alias == "" {
		alias = strings.TrimSpace(meta.DatabaseAlias)
	}
	if alias == "" {
		alias = a.cfg.DefaultAlias
	}

	key := alias + "::" + meta.Name
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.stores[key]; ok {
		return s, nil
	}

	handle, dialect, err := a.cfg.Resolve(alias)
	if err != nil {
		return nil, fmt.Errorf("datasource/nucleus: resolve alias %q for model %q: %w", alias, meta.Name, err)
	}
	sqlDB, err := handle.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("datasource/nucleus: sql handle alias %q model %q: %w", alias, meta.Name, err)
	}

	crud := model.NewCRUD(sqlDB, meta, a.cfg.Bus)
	if a.cfg.Observer != nil && (a.cfg.BusConnected == nil || !a.cfg.BusConnected()) {
		crud.SetSQLQueryObserver(a.cfg.Observer)
	}
	if dialect != "" {
		crud.SetDialect(dialect)
	}

	s := &store{meta: meta, crud: crud, sqlDB: sqlDB, dialect: strings.ToLower(dialect)}
	a.stores[key] = s
	return s, nil
}

// toModelInfo maps Nucleus metadata to the neutral ModelInfo.
func toModelInfo(m *model.ModelMeta) datasource.ModelInfo {
	fields := make([]datasource.FieldInfo, 0, len(m.Fields))
	for _, f := range m.Fields {
		fields = append(fields, toFieldInfo(f))
	}

	fks := make([]datasource.ForeignKey, 0, len(m.ForeignKeys))
	for _, fk := range m.ForeignKeys {
		fks = append(fks, datasource.ForeignKey{
			FieldName:     fk.FieldName,
			Column:        fk.Column,
			ForeignModel:  fk.ForeignModel,
			ForeignTable:  fk.ForeignTable,
			ForeignColumn: fk.ForeignColumn,
		})
	}

	idxs := make([]datasource.Index, 0, len(m.Indexes))
	for _, ix := range m.Indexes {
		idxs = append(idxs, datasource.Index{Name: ix.Name, Columns: append([]string(nil), ix.Columns...), Unique: ix.Unique})
	}

	return datasource.ModelInfo{
		Name:          m.Name,
		Plural:        m.Plural,
		Table:         m.Table,
		PrimaryKey:    m.PrimaryKey,
		DatabaseAlias: m.DatabaseAlias,
		Icon:          m.Config.Icon,
		ReadOnly:      m.Config.ReadOnly,
		TenantField:   m.TenantFieldName(),
		Fields:        fields,
		ForeignKeys:   fks,
		Indexes:       idxs,
	}
}

func toFieldInfo(f model.FieldMeta) datasource.FieldInfo {
	var choices []datasource.Choice
	if len(f.Choices) > 0 {
		choices = make([]datasource.Choice, 0, len(f.Choices))
		for _, c := range f.Choices {
			choices = append(choices, datasource.Choice{Value: c.Value, Label: c.Label})
		}
	}
	return datasource.FieldInfo{
		Name:          f.Name,
		Column:        f.Column,
		Label:         f.Label,
		GoType:        f.GoType,
		HTMLType:      f.HTMLType,
		IsPK:          f.IsPK,
		IsRequired:    f.IsRequired,
		IsReadOnly:    f.IsReadOnly,
		IsList:        f.IsList,
		IsSearch:      f.IsSearch,
		IsFilter:      f.IsFilter,
		IsExcluded:    f.IsExcluded,
		IsForeignKey:  f.IsForeignKey,
		IsTenantField: f.IsTenantField,
		ForeignModel:  f.ForeignModel,
		Choices:       choices,
	}
}
