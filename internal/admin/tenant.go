package admin

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
)

// tenantContextKey is the context key for tenant information.
type tenantContextKey struct{}

// TenantContext holds the tenant resolution of one admin request. It is
// built by tenantContextMiddleware from, in order of precedence, the
// ?tenant= override (gated and audited), the tenant the host resolved for
// the request (PanelConfig.TenantResolver) and the configured default.
type TenantContext struct {
	Enabled     bool   // Whether multi-tenant is enabled
	TenantID    string // Current tenant ID (empty = global/all tenants)
	TenantField string // The column name for tenant isolation
	AutoFilter  bool   // Whether Data Studio is confined to TenantID
	Overridden  bool   // TenantID came from ?tenant= (superuser / tenant_switch)
}

// tenantSwitchAction is the RBAC action (on resource "admin:*") that lets
// a non-superuser operator look at another tenant, or at all of them,
// through ?tenant=.
const tenantSwitchAction = "tenant_switch"

// tenantContextKey is used to retrieve/set tenant context in request context.
var adminTenantCtxKey = tenantContextKey{}

// tenantContextFromRequest extracts tenant context from request.
func tenantContextFromRequest(r *http.Request) *TenantContext {
	if r == nil {
		return &TenantContext{Enabled: false}
	}
	ctx, ok := r.Context().Value(adminTenantCtxKey).(*TenantContext)
	if !ok || ctx == nil {
		return &TenantContext{Enabled: false}
	}
	return ctx
}

// requestTenant extracts the tenant query parameter from the request.
func requestTenant(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return strings.TrimSpace(r.URL.Query().Get("tenant"))
}

// canSwitchTenant reports whether the request's operator may look at a
// tenant other than the one the host resolved (?tenant=<id>) or at every
// tenant at once (?tenant=all): a superuser, or a subject granted the
// tenant_switch action on admin:* when an RBAC enforcer is configured. The
// open posture (no auth provider, warned at mount) has no operator to gate.
func (p *Panel) canSwitchTenant(r *http.Request) bool {
	if p.config.Auth == nil {
		return true
	}
	user, err := p.authenticatedUser(r)
	if err != nil || user == nil {
		return false
	}
	if user.IsSuperuser {
		return true
	}
	if p.rbac != nil {
		for _, subject := range []string{user.ID, user.Role, user.Username} {
			if subject != "" && p.rbac.Can(subject, "admin:*", tenantSwitchAction) {
				return true
			}
		}
	}
	return false
}

// tenantScope is the tenant confinement of one request for one model: the
// model's tenant column, the tenant the request is scoped to, and the keys
// the backend emits or accepts for that column. A zero scope (Enforced()
// false) means the request sees every row — multi-tenant off, no tenant
// resolved, a superuser on ?tenant=all, or a model without a tenant column.
type tenantScope struct {
	Field  datasource.FieldInfo
	Tenant string
	// Keys are every key the backend resolves to Field: its storage column,
	// runtime column and Go name, and — for a Nucleus model — the json key
	// its records carry, which its adapter accepts on input too. The guard
	// and the tenant read look the field up through this list, so a key
	// the backend honours is never one the guard does not know.
	Keys []string
}

// newTenantScope builds the confinement of tenant over field, adding
// jsonKey (may be empty) to the keys the field is looked up by.
func newTenantScope(field datasource.FieldInfo, tenant, jsonKey string) tenantScope {
	return tenantScope{Field: field, Tenant: tenant, Keys: fieldKeys(field, jsonKey)}
}

// Enforced reports whether the scope confines the request.
func (s tenantScope) Enforced() bool {
	return s.Tenant != "" && s.Field.Column != ""
}

// Column is the runtime column name the list filter uses.
func (s tenantScope) Column() string {
	return runtimeColumn(s.Field.Column)
}

// canonicalTenant renders a tenant value the way the scope compares it: a
// number or a json.Number as its decimal text (a numeric tenant column
// arrives as float64 from the JSON record), a string, []byte or Stringer
// exactly as it is. It never trims: both adapters store the tenant value
// verbatim, so ' acme ' is a tenant of its own — one no request resolves to
// — and a guard that compared it trimmed let a scoped operator create or
// move rows into it. It returns false for nil and for an empty string.
func canonicalTenant(v any) (string, bool) {
	switch n := v.(type) {
	case string:
		return n, n != ""
	case []byte:
		return string(n), len(n) != 0
	case fmt.Stringer:
		s := n.String()
		return s, s != ""
	default:
		return canonicalID(v)
	}
}

// recordTenant returns the tenant rec carries under one of the scope's
// keys, in canonical string form (canonicalTenant), and whether rec
// carries the field at all. A record without the field is not a record of
// another tenant: both adapters leave a field hidden from JSON (json:"-")
// out of the records they emit, so the caller confirms membership through
// the store (owns).
func (s tenantScope) recordTenant(rec datasource.Record) (tenant string, present bool) {
	v, ok := recordValueByKeys(rec, s.Keys)
	if !ok {
		return "", false
	}
	tenant, _ = canonicalTenant(v)
	return tenant, true
}

// owns reports whether the row id names — rec being the record st returned
// for it — belongs to the scope's tenant. A record that carries the tenant
// field is compared in place. One that carries it under none of the
// scope's keys (the field is hidden from JSON, so the record has no tenant
// key at all) is confirmed through the store: a list filtered by the tenant
// column and the primary key answers the row only when it is the tenant's.
// The row that list returns must name id as its primary key — the Nucleus
// backend drops a filter column it cannot resolve instead of refusing it,
// and a list confined by tenant alone would answer the tenant's first row
// for any id. A model whose primary key the scope cannot resolve never
// confirms a row this way.
func (s tenantScope) owns(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, rec datasource.Record) (bool, error) {
	if tenant, present := s.recordTenant(rec); present {
		return tenant == s.Tenant, nil
	}
	want, ok := canonicalID(id)
	if !ok {
		return false, nil
	}
	pkColumn, _, ok := dsResolveField(mi, mi.PrimaryKey)
	if !ok {
		return false, nil
	}
	page, err := st.List(ctx, datasource.Query{
		Page:     1,
		PageSize: 1,
		Filters:  map[string]string{s.Column(): s.Tenant, pkColumn: want},
	})
	if err != nil {
		return false, err
	}
	for _, item := range page.Items {
		if got, ok := canonicalID(recordPKValue(item, mi)); ok && got == want {
			return true, nil
		}
	}
	return false, nil
}

// fieldPayloadKeys returns, sorted, the keys of data the backend resolves
// to field f by its column or Go name. The Nucleus adapter matches a
// payload key against the storage column and the Go field name
// case-insensitively, so a guard that looks the key up exactly is not a
// guard: TENANT_ID slipped past it and reached the backend, which wrote the
// row in the other tenant. The tenant guard adds the field's json key
// through tenantScope.Keys; see matchingKeys.
func fieldPayloadKeys(f datasource.FieldInfo, data map[string]any) []string {
	return matchingKeys(fieldKeys(f), data)
}

// matchingKeys returns, sorted, the keys of data that equal one of
// candidates in any letter case.
func matchingKeys(candidates []string, data map[string]any) []string {
	var keys []string
	for key := range data {
		for _, candidate := range candidates {
			if strings.EqualFold(key, candidate) {
				keys = append(keys, key)
				break
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// payloadTenant returns the tenant a write payload names under any key the
// backend resolves to the tenant field (see tenantScope.Keys), in any
// letter case, the key it names it under, and whether it names one at all.
// The value is compared exactly (canonicalTenant): the adapters store it
// verbatim. A payload naming it under two keys is refused: the backend
// would keep whichever map order hands it last.
func (s tenantScope) payloadTenant(data map[string]any) (tenant, key string, named bool, err error) {
	keys := matchingKeys(s.Keys, data)
	switch len(keys) {
	case 0:
		return "", "", false, nil
	case 1:
		tenant, _ = canonicalTenant(data[keys[0]])
		return tenant, keys[0], true, nil
	default:
		return "", "", true, gferrors.BadRequest("tenant field " + s.Field.Column + " appears more than once in the payload (" + strings.Join(keys, ", ") + ")")
	}
}

// guardPayload confines a write payload to the scope's tenant: a payload
// naming another tenant — a padded spelling of the own tenant included —
// or the tenant under two spellings, is refused (400); one naming none has
// the tenant stamped when stamp is set (creates and imports; an update
// leaves the row's tenant alone). One naming the own tenant has the value
// replaced under its key by the resolved tenant, so what reaches the
// adapter is the scope's tenant and not the payload's rendering of it (a
// number for a numeric tenant column, say).
func (s tenantScope) guardPayload(data map[string]any, stamp bool) error {
	got, key, named, err := s.payloadTenant(data)
	if err != nil {
		return err
	}
	if !named {
		if stamp {
			data[s.Field.Column] = s.Tenant
		}
		return nil
	}
	if got != s.Tenant {
		return tenantChangeError(s, got)
	}
	data[key] = s.Tenant
	return nil
}

// importTenantScope is the confinement an import, a fixture load or an
// export applies when it targets a tenant: the model's own tenant column
// and that tenant. Zero when the model has no tenant column or no tenant is
// targeted.
func (p *Panel) importTenantScope(mi datasource.ModelInfo, tenant string) tenantScope {
	if mi.TenantField == "" || tenant == "" {
		return tenantScope{}
	}
	if _, field, ok := dsResolveField(mi, mi.TenantField); ok {
		return newTenantScope(field, tenant, p.fieldJSONKey(mi.Name, field))
	}
	return newTenantScope(datasource.FieldInfo{Column: mi.TenantField, Name: mi.TenantField}, tenant, "")
}

// fieldJSONKey returns the key a Nucleus model's records carry field f
// under — the json tag of its struct field, which the Nucleus adapter also
// accepts on input — or "" when the panel has no schema registry (a custom
// DataSource, Quark), the model is not in it, or the field is excluded from
// JSON (json:"-"). A field without a tag marshals under its Go name, which
// the lookup already covers.
//
// The Quark adapter emits records keyed by storage column and resolves
// update keys by column or Go name only; a json tag that differs from both
// is unknown to this panel there, and a create naming it next to the
// stamped tenant is refused by the adapter itself (two keys, one field).
func (p *Panel) fieldJSONKey(modelName string, f datasource.FieldInfo) string {
	if p.registry == nil || f.Name == "" {
		return ""
	}
	meta, ok := p.registry.Get(modelName)
	if !ok || meta == nil || meta.Type == nil {
		return ""
	}
	typ := meta.Type
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return ""
	}
	sf, ok := typ.FieldByName(f.Name)
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	if name == "-" {
		return ""
	}
	return name
}

// requestTenantScope resolves the tenant confinement of r for model mi. It is
// enforced only when multi-tenant mode is on, the request resolved a tenant
// (AutoFilter stays on) and the model has the tenant column — a configured
// tenant field the model lacks cannot scope it, and the list filter would be
// dropped silently by the backend anyway.
func (p *Panel) requestTenantScope(r *http.Request, mi datasource.ModelInfo) tenantScope {
	tc := tenantContextFromRequest(r)
	if tc == nil || !tc.Enabled || !tc.AutoFilter || tc.TenantID == "" {
		return tenantScope{}
	}
	column := p.resolveTenantField(mi.Name)
	if column == "" {
		return tenantScope{}
	}
	_, field, ok := dsResolveField(mi, column)
	if !ok {
		return tenantScope{}
	}
	return newTenantScope(field, tc.TenantID, p.fieldJSONKey(mi.Name, field))
}

// enforcedTenantID is the tenant every model-agnostic operation (exports,
// imports, fixtures) is confined to, or "" when the request is not scoped.
// When it is set it overrides whatever tenant_id the request body names:
// choosing another tenant goes through ?tenant=, which is gated and audited.
func (p *Panel) enforcedTenantID(r *http.Request) string {
	tc := tenantContextFromRequest(r)
	if tc == nil || !tc.Enabled || !tc.AutoFilter {
		return ""
	}
	return tc.TenantID
}

// scopedRecord loads id and confirms it belongs to the request's tenant
// (tenantScope.owns). A row of another tenant is reported as not found —
// the same answer as a row that does not exist, so the id space of other
// tenants is not disclosed.
func scopedRecord(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, scope tenantScope) (datasource.Record, error) {
	rec, err := st.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.Enforced() {
		owned, err := scope.owns(ctx, st, mi, id, rec)
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, gferrors.NotFound(mi.Name, id)
		}
	}
	return rec, nil
}

// tenantChangeError is the 400 a write gets when it tries to move a row to
// another tenant, or to create one in another tenant, from a scoped request.
func tenantChangeError(scope tenantScope, got string) error {
	return gferrors.BadRequest("tenant field " + scope.Field.Column + " must be " + scope.Tenant + " in this request, got " + got + " (switching tenant requires ?tenant= and the permission to use it)")
}
