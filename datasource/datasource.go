// Package datasource is Orbit's neutral, backend-agnostic contract for Data
// Studio (ADR-001). The Data Studio panel speaks only these types and never
// imports nucleus/pkg/model or pkg/db; a single adapter per backend translates
// — internal/datasource/nucleus today, a Quark adapter later.
//
// These interfaces and types are Orbit public API and are frozen at Orbit v1.0
// (quantum/QADR-0005). Design decisions (ADR-001):
//
//   - D1 — IDs are string at the boundary (Quark PKs may be uuid/string/
//     composite); a backend adapter narrows internally.
//   - D2 — the panel speaks Record (maps), not entities; the reflection lives in
//     the adapter, which enables non-struct backends.
//   - D3 — catalogue (ModelSource) and access (RecordStore) are separate; Store
//     resolves a store per (model, dbAlias) request.
package datasource

import "context"

// Record is one row as a neutral map. An adapter builds it so it marshals to
// the exact JSON object the backend's native row would (the Nucleus adapter
// uses a struct→JSON round-trip), so the embedded SPA reads it unchanged. Keys
// are a field's storage column or Go name — the SPA looks up either.
type Record = map[string]any

// Choice is a select/enum option for a field.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FieldInfo describes one field of a model, backend-neutral. It carries exactly
// what Data Studio needs to render columns, forms, filters, and exports; the
// HTTP layer maps it to the SPA-facing schema JSON.
type FieldInfo struct {
	Name          string   `json:"name"`
	Column        string   `json:"column"`
	Label         string   `json:"label"`
	GoType        string   `json:"go_type"`
	HTMLType      string   `json:"html_type"`
	IsPK          bool     `json:"is_pk"`
	IsRequired    bool     `json:"is_required"`
	IsReadOnly    bool     `json:"is_read_only"`
	IsList        bool     `json:"is_list"`
	IsSearch      bool     `json:"is_search"`
	IsFilter      bool     `json:"is_filter"`
	IsExcluded    bool     `json:"is_excluded"`
	IsForeignKey  bool     `json:"is_foreign_key"`
	IsTenantField bool     `json:"is_tenant_field"`
	ForeignModel  string   `json:"foreign_model,omitempty"`
	Choices       []Choice `json:"choices,omitempty"`
}

// ForeignKey is a detected relationship, neutral.
type ForeignKey struct {
	FieldName     string `json:"field_name"`
	Column        string `json:"column"`
	ForeignModel  string `json:"foreign_model"`
	ForeignTable  string `json:"foreign_table"`
	ForeignColumn string `json:"foreign_column"`
}

// Index is a declared index, neutral. Data Studio uses unique indexes to detect
// existing records on import.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

// ModelInfo is a backend-neutral description of a model. TenantField is the
// resolved tenant column ("" when the model is not tenant-scoped).
type ModelInfo struct {
	Name          string       `json:"name"`
	Plural        string       `json:"plural"`
	Table         string       `json:"table"`
	PrimaryKey    string       `json:"primary_key"`
	DatabaseAlias string       `json:"database_alias"`
	Icon          string       `json:"icon"`
	ReadOnly      bool         `json:"read_only"`
	TenantField   string       `json:"tenant_field"`
	Fields        []FieldInfo  `json:"fields"`
	ForeignKeys   []ForeignKey `json:"foreign_keys"`
	Indexes       []Index      `json:"indexes"`
}

// Field returns the FieldInfo whose column or Go name matches key
// (case-insensitive), and whether it was found. It is the neutral replacement
// for the panel's old resolveField over model.FieldMeta.
func (m ModelInfo) Field(key string) (FieldInfo, bool) {
	for _, f := range m.Fields {
		if equalFold(key, f.Column) || equalFold(key, f.Name) {
			return f, true
		}
	}
	return FieldInfo{}, false
}

// ModelSource is the catalogue half of a DataSource: model discovery and lookup.
type ModelSource interface {
	All() []ModelInfo
	Get(name string) (ModelInfo, bool)
}

// Query is a neutral list query. Filters are column→value exact matches (the
// adapter applies backend-appropriate escaping); OrderBy is a comma-separated
// "col [asc|desc]" list validated against the model's columns.
type Query struct {
	Page     int
	PageSize int
	Search   string
	Filters  map[string]string
	OrderBy  string
}

// Page is a slice of records plus pagination metadata. Its JSON shape is frozen
// to what the embedded SPA already consumes (ADR-001 O3): the Nucleus backend's
// native paginated envelope had exactly these keys, so the SPA is unchanged.
type Page struct {
	Items       []Record `json:"items"`
	Total       int64    `json:"total"`
	Page        int      `json:"page"`
	PageSize    int      `json:"page_size"`
	TotalPages  int      `json:"total_pages"`
	IsEstimated bool     `json:"is_estimated"`
	HasMore     bool     `json:"has_more"`
}

// CountResult is a (possibly estimated) row count. Present is false when the
// backing table does not exist.
type CountResult struct {
	Count       int64
	IsEstimated bool
	Present     bool
}

// RecordStore is the access half: CRUD over one (model, dbAlias) pair. IDs are
// strings at the boundary (D1).
type RecordStore interface {
	List(ctx context.Context, q Query) (Page, error)
	Get(ctx context.Context, id string) (Record, error)
	Create(ctx context.Context, rec Record) (Record, error)
	Update(ctx context.Context, id string, rec Record) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (CountResult, error)
	TableExists(ctx context.Context) bool
}

// DataSource is what NewPanel takes. Store resolves the RecordStore for a model
// and database alias (empty alias = the model's default), mirroring the panel's
// old getCRUD(meta, alias).
type DataSource interface {
	ModelSource
	Store(modelName, dbAlias string) (RecordStore, error)
}

// equalFold is a tiny ASCII-friendly case-insensitive compare kept local so the
// contract package has zero imports beyond context.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
