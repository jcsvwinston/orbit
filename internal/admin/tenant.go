package admin

import (
	"context"
	"net/http"
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
// model's tenant column and the tenant the request is scoped to. A zero
// scope (Enforced() false) means the request sees every row — multi-tenant
// off, no tenant resolved, a superuser on ?tenant=all, or a model without
// a tenant column.
type tenantScope struct {
	Field  datasource.FieldInfo
	Tenant string
}

// Enforced reports whether the scope confines the request.
func (s tenantScope) Enforced() bool {
	return s.Tenant != "" && s.Field.Column != ""
}

// Column is the runtime column name the list filter uses.
func (s tenantScope) Column() string {
	return runtimeColumn(s.Field.Column)
}

// contains reports whether rec belongs to the scope's tenant. Values are
// compared in their canonical string form (a numeric tenant column arrives
// as float64 from the JSON record), so it holds for Nucleus and Quark alike.
func (s tenantScope) contains(rec datasource.Record) bool {
	v, ok := recordValue(rec, s.Field)
	if !ok {
		return false
	}
	id, ok := canonicalID(v)
	return ok && id == s.Tenant
}

// fieldPayloadKeys returns, sorted, the keys of data the backend resolves
// to field f. The Nucleus adapter matches a payload key against the storage
// column and the Go field name case-insensitively, so a guard that looks
// the key up exactly is not a guard: TENANT_ID slipped past it and reached
// the backend, which wrote the row in the other tenant.
func fieldPayloadKeys(f datasource.FieldInfo, data map[string]any) []string {
	var keys []string
	for key := range data {
		for _, candidate := range []string{f.Column, runtimeColumn(f.Column), f.Name} {
			if candidate != "" && strings.EqualFold(key, candidate) {
				keys = append(keys, key)
				break
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// payloadTenant returns the tenant a write payload names under the tenant
// column or its Go field name, in any letter case, and whether it names one
// at all. A payload naming it under two spellings is refused: the backend
// would keep whichever map order hands it last.
func (s tenantScope) payloadTenant(data map[string]any) (tenant string, named bool, err error) {
	keys := fieldPayloadKeys(s.Field, data)
	switch len(keys) {
	case 0:
		return "", false, nil
	case 1:
		id, _ := canonicalID(data[keys[0]])
		return id, true, nil
	default:
		return "", true, gferrors.BadRequest("tenant field " + s.Field.Column + " appears more than once in the payload (" + strings.Join(keys, ", ") + ")")
	}
}

// guardPayload confines a write payload to the scope's tenant: a payload
// naming another tenant, or the tenant under two spellings, is refused
// (400); one naming none has the tenant stamped when stamp is set (creates
// and imports; an update leaves the row's tenant alone).
func (s tenantScope) guardPayload(data map[string]any, stamp bool) error {
	got, named, err := s.payloadTenant(data)
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
	return nil
}

// importTenantScope is the confinement an import, a fixture load or an
// export applies when it targets a tenant: the model's own tenant column
// and that tenant. Zero when the model has no tenant column or no tenant is
// targeted.
func importTenantScope(mi datasource.ModelInfo, tenant string) tenantScope {
	if mi.TenantField == "" || tenant == "" {
		return tenantScope{}
	}
	if _, field, ok := dsResolveField(mi, mi.TenantField); ok {
		return tenantScope{Field: field, Tenant: tenant}
	}
	return tenantScope{Field: datasource.FieldInfo{Column: mi.TenantField, Name: mi.TenantField}, Tenant: tenant}
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
	return tenantScope{Field: field, Tenant: tc.TenantID}
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

// scopedRecord loads id and confirms it belongs to the request's tenant. A
// row of another tenant is reported as not found — the same answer as a
// row that does not exist, so the id space of other tenants is not
// disclosed.
func scopedRecord(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, scope tenantScope) (datasource.Record, error) {
	rec, err := st.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.Enforced() && !scope.contains(rec) {
		return nil, gferrors.NotFound(mi.Name, id)
	}
	return rec, nil
}

// tenantChangeError is the 400 a write gets when it tries to move a row to
// another tenant, or to create one in another tenant, from a scoped request.
func tenantChangeError(scope tenantScope, got string) error {
	return gferrors.BadRequest("tenant field " + scope.Field.Column + " must be " + scope.Tenant + " in this request, got " + got + " (switching tenant requires ?tenant= and the permission to use it)")
}
