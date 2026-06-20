package admin

import (
	"context"
	"net/http"
	"strings"
)

// tenantContextKey is the context key for tenant information.
type tenantContextKey struct{}

// requestScopeCtxKey mirrors the app package's requestScopeCtxKey for type-safe extraction.
// We use reflection-style extraction since the admin package doesn't import app directly.
type requestScope struct {
	Host          string
	Site          string
	Tenant        string
	DatabaseAlias string
}

type requestScopeKey struct{}

// TenantContext holds tenant resolution data for admin operations.
type TenantContext struct {
	Enabled     bool   // Whether multi-tenant is enabled
	TenantID    string // Current tenant ID (empty = global/all tenants)
	TenantField string // The column name for tenant isolation
	AutoFilter  bool   // Whether to auto-filter queries by tenant
}

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

// requestScopeFromContext extracts the request scope (site/tenant/db) from context.
// This mirrors the app package's RequestScopeFromContext functionality.
func requestScopeFromContext(ctx context.Context) (requestScope, bool) {
	if ctx == nil {
		return requestScope{}, false
	}
	// Try the admin tenant context key first (set by our middleware)
	if tc, ok := ctx.Value(adminTenantCtxKey).(*TenantContext); ok && tc != nil {
		return requestScope{Tenant: tc.TenantID}, true
	}
	// Try the request scope key (set by app middleware)
	if scope, ok := ctx.Value(requestScopeKey{}).(requestScope); ok {
		return scope, true
	}
	return requestScope{}, false
}
