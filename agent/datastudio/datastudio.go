// Package datastudio serves Data Studio requests on the agent side of the
// fleet plane through Orbit's datasource contract (ADR-001, ADR-002): the
// same DataSource the in-process panel speaks, so a Nucleus registry, a
// Quark data source or any third-party implementation answers the fleet the
// way it answers the panel.
//
// Until A9 S4 this package built its own model.CRUD path over the agent's
// database handles, with no operator behind the request: the fleet was a
// second Data Studio with its own semantics and none of the application's
// authorization. Now the request carries the operator the admin server
// resolved (DataStudioRequest.operator, sent by servers from v0.14.1 on) and
// the handler runs it as the panel would run it for that operator:
//
//   - the framework identity reaches the model layer's hooks
//     (auth.ClaimsFromContext sees the operator's subject and role);
//   - the application's policy applies per model and verb through the
//     agent's Authorizer — the same verbs the panel uses: list, retrieve,
//     create, update, delete, bulk_delete;
//   - a tenant-scoped operator is confined to its tenant: reads carry an
//     equality filter on the model's tenant column, a create is stamped with
//     the tenant, and an update or delete first confirms the row is the
//     tenant's.
//
// A request that carries no operator — an older server — behaves as before:
// no identity, no policy, no tenant. The server's own gates (the mutation
// allowlist, the read-only role) stay in front in both cases.
package datastudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"

	"github.com/jcsvwinston/orbit/agent/rbac"
	"github.com/jcsvwinston/orbit/datasource"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// Config is what the handler needs to serve Data Studio.
type Config struct {
	// Source answers every model and record question. Nil disables the
	// handler (the agent reports Data Studio as not enabled on this node).
	Source datasource.DataSource

	// DefaultAlias is used when a request's database_alias is empty.
	// Defaults to "default".
	DefaultAlias string

	// Authorizer is the application's policy, consulted per model and
	// verb for the operator a request carries. Nil (or a request without
	// an operator) means no per-model authorization on the agent — the
	// server's gates alone, as before ADR-002.
	Authorizer rbac.PolicySource

	// Logger receives diagnostics. Nil means slog.Default.
	Logger *slog.Logger
}

func (c Config) defaultAlias() string {
	if a := strings.TrimSpace(c.DefaultAlias); a != "" {
		return a
	}
	return "default"
}

// Decider is the half of an authorizer that answers a question. Nucleus's
// *authz.Enforcer implements it; a PolicySource that only exposes its rows
// is compiled into one on first use.
type Decider interface {
	Can(sub, obj, act string) bool
}

// Handler dispatches DataStudioRequest frames. It is goroutine-safe; the
// agent's stream layer calls Dispatch from its receive loop.
type Handler struct {
	cfg    Config
	logger *slog.Logger

	once    sync.Once
	decider Decider
	decErr  error
}

// New constructs a Handler. Returns nil when cfg.Source is nil (caller
// treats this as "Data Studio disabled on this agent").
func New(cfg Config) *Handler {
	if cfg.Source == nil {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{cfg: cfg, logger: logger}
}

// RegisteredModels returns the names of every model this handler can
// serve. The agent ships this list in NodeRegistration so the admin server
// can route requests to the right node.
func (h *Handler) RegisteredModels() []string {
	if h == nil || h.cfg.Source == nil {
		return nil
	}
	all := h.cfg.Source.All()
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Name)
	}
	return out
}

// Dispatch executes the request and returns the response. The response
// always carries the same RequestId; on failure, Error is non-empty and
// Body is nil. Dispatch never returns nil.
//
// Two error prefixes are a wire convention the admin server maps onto
// Connect codes: "permission denied:" → PermissionDenied and "not found:"
// → NotFound. Everything else is Unknown.
func (h *Handler) Dispatch(ctx context.Context, req *adminv1.DataStudioRequest) *adminv1.DataStudioResponse {
	resp := &adminv1.DataStudioResponse{RequestId: req.GetRequestId()}

	if h == nil || h.cfg.Source == nil {
		resp.Error = "admin agent: data studio is not enabled on this node"
		return resp
	}
	op := req.GetOperator()
	ctx = withOperator(ctx, op)

	switch body := req.GetBody().(type) {
	case *adminv1.DataStudioRequest_ListModels:
		h.handleListModels(ctx, resp, op, body.ListModels)
	case *adminv1.DataStudioRequest_GetSchema:
		h.handleGetSchema(resp, op, body.GetSchema)
	case *adminv1.DataStudioRequest_ListRecords:
		h.handleListRecords(ctx, resp, op, body.ListRecords)
	case *adminv1.DataStudioRequest_GetRecord:
		h.handleGetRecord(ctx, resp, op, body.GetRecord)
	case *adminv1.DataStudioRequest_CreateRecord:
		h.handleCreateRecord(ctx, resp, op, body.CreateRecord)
	case *adminv1.DataStudioRequest_UpdateRecord:
		h.handleUpdateRecord(ctx, resp, op, body.UpdateRecord)
	case *adminv1.DataStudioRequest_DeleteRecord:
		h.handleDeleteRecord(ctx, resp, op, body.DeleteRecord)
	case *adminv1.DataStudioRequest_BulkAction:
		h.handleBulkAction(ctx, resp, op, body.BulkAction)
	default:
		resp.Error = fmt.Sprintf("admin agent: unsupported data studio request: %T", body)
	}
	return resp
}

// =============================================================================
// the operator: identity, policy, tenant
// =============================================================================

// withOperator puts the operator on the context the way the framework's
// own request pipeline would, so a model hook (BeforeCreate, ...) that
// asks auth.ClaimsFromContext sees who is asking. No operator, no claims.
func withOperator(ctx context.Context, op *adminv1.OperatorIdentity) context.Context {
	if op == nil || strings.TrimSpace(op.GetSubject()) == "" {
		return ctx
	}
	return auth.ContextWithClaims(ctx, &auth.Claims{
		UserID:   op.GetSubject(),
		Username: op.GetSubject(),
		Role:     op.GetRole(),
	})
}

// authorize answers whether the operator may perform verb on the model,
// with the panel's rule: no policy configured means allowed; with a policy,
// the subject or its role must be granted `<verb>` on `admin:<Model>`.
func (h *Handler) authorize(op *adminv1.OperatorIdentity, modelName, verb string) error {
	if op == nil || strings.TrimSpace(op.GetSubject()) == "" || h.cfg.Authorizer == nil {
		return nil
	}
	if op.GetReadOnly() && verb != "list" && verb != "retrieve" {
		return fmt.Errorf("permission denied: operator %q is read-only", op.GetSubject())
	}
	d, err := h.deciderFor()
	if err != nil {
		return fmt.Errorf("permission denied: the application's policy could not be loaded: %v", err)
	}
	resource := "admin:" + modelName
	if d.Can(op.GetSubject(), resource, verb) {
		return nil
	}
	if role := strings.TrimSpace(op.GetRole()); role != "" && d.Can(role, resource, verb) {
		return nil
	}
	return fmt.Errorf("permission denied: %q may not %s %s", op.GetSubject(), verb, modelName)
}

// deciderFor returns the policy as something that can answer. An
// Authorizer that decides for itself (nucleus's Enforcer) is used as is; one
// that only exposes its rows is compiled into an in-memory enforcer once —
// allow rows become grants, rows whose fourth column says deny become
// denials, grouping rows become roles.
func (h *Handler) deciderFor() (Decider, error) {
	h.once.Do(func() {
		if d, ok := h.cfg.Authorizer.(Decider); ok {
			h.decider = d
			return
		}
		enf, err := authz.New(h.logger)
		if err != nil {
			h.decErr = err
			return
		}
		rows, err := h.cfg.Authorizer.GetPolicy()
		if err != nil {
			h.decErr = err
			return
		}
		for _, row := range rows {
			if len(row) < 3 {
				continue
			}
			if len(row) >= 4 && strings.EqualFold(strings.TrimSpace(row[3]), "deny") {
				err = enf.Deny(row[0], row[1], row[2])
			} else {
				err = enf.AddPolicy(row[0], row[1], row[2])
			}
			if err != nil {
				h.decErr = err
				return
			}
		}
		groups, err := h.cfg.Authorizer.GetGroupingPolicy()
		if err != nil {
			h.decErr = err
			return
		}
		for _, g := range groups {
			if len(g) >= 2 {
				if err := enf.AddRole(g[0], g[1]); err != nil {
					h.decErr = err
					return
				}
			}
		}
		h.decider = enf
	})
	return h.decider, h.decErr
}

// tenantScope is the confinement of one request for one model: the
// tenant the operator is scoped to and the model's tenant field. A zero
// scope (Enforced false) means the request sees every row — no tenant on
// the operator, or a model without a tenant field.
type tenantScope struct {
	Tenant string
	Field  datasource.FieldInfo
}

func (s tenantScope) Enforced() bool { return s.Tenant != "" && s.Field.Column != "" }

func scopeFor(op *adminv1.OperatorIdentity, mi datasource.ModelInfo) tenantScope {
	if op == nil || strings.TrimSpace(op.GetTenant()) == "" || mi.TenantField == "" {
		return tenantScope{}
	}
	f, ok := mi.Field(mi.TenantField)
	if !ok {
		f = datasource.FieldInfo{Name: mi.TenantField, Column: mi.TenantField}
	}
	return tenantScope{Tenant: strings.TrimSpace(op.GetTenant()), Field: f}
}

// owns reports whether rec — a record the store returned — belongs to the
// scope's tenant. The record is keyed by whatever the data source emits
// (the Nucleus adapter: the field's JSON key), so the tenant is looked up
// by the field's name and column, folded.
func (s tenantScope) owns(rec datasource.Record) bool {
	v, ok := lookup(rec, s.Field.Name, s.Field.Column)
	if !ok {
		return false
	}
	return fmt.Sprint(v) == s.Tenant
}

// =============================================================================
// list_models / get_schema (read-only metadata)
// =============================================================================

func (h *Handler) handleListModels(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.ListModelsRequest) {
	all := h.cfg.Source.All()
	out := make([]*adminv1.ModelInfo, 0, len(all))
	alias := strings.TrimSpace(req.GetDatabaseAlias())

	for _, m := range all {
		if alias != "" && m.DatabaseAlias != "" && !strings.EqualFold(alias, m.DatabaseAlias) {
			continue
		}
		info := modelToProto(m)
		info.RecordCount = -1
		if req.GetIncludeCounts() && h.authorize(op, m.Name, "list") == nil {
			if st, err := h.cfg.Source.Store(m.Name, h.aliasFor(alias, m)); err == nil {
				if c, err := st.Count(ctx); err == nil && c.Present {
					info.RecordCount = c.Count
					info.RecordCountEstimated = c.IsEstimated
				}
			}
		}
		out = append(out, info)
	}
	resp.Body = &adminv1.DataStudioResponse_ListModels{
		ListModels: &adminv1.ListModelsResponse{Models: out},
	}
}

func (h *Handler) handleGetSchema(resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.GetSchemaRequest) {
	mi, ok := h.cfg.Source.Get(req.GetModelName())
	if !ok {
		resp.Error = fmt.Sprintf("not found: model %q is not registered", req.GetModelName())
		return
	}
	if err := h.authorize(op, mi.Name, "get_schema"); err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Schema{
		Schema: &adminv1.ModelSchema{
			Info:   modelToProto(mi),
			Fields: fieldsToProto(mi.Fields),
		},
	}
}

// =============================================================================
// list_records / get_record
// =============================================================================

func (h *Handler) handleListRecords(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.ListRecordsRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := h.authorize(op, mi.Name, "list"); err != nil {
		resp.Error = err.Error()
		return
	}
	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 {
		pageSize = 25
	}
	where, err := whereFromWire(req.GetWhere())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	filters := cloneFilters(req.GetFilters())
	// A tenant-scoped operator sees the tenant's rows: the scope is written
	// over any filter the request carried on the same column, not merged.
	if scope := scopeFor(op, mi); scope.Enforced() {
		if filters == nil {
			filters = map[string]string{}
		}
		filters[scope.Field.Column] = scope.Tenant
	}
	res, err := st.List(ctx, datasource.Query{
		Page:     page,
		PageSize: pageSize,
		Search:   req.GetSearch(),
		Filters:  filters,
		OrderBy:  req.GetOrderBy(),
		Where:    where,
		// The fleet UI is a screen with a pager: it needs to know how many
		// pages there are, filtered or not.
		ExactTotal: true,
	})
	if err != nil {
		resp.Error = err.Error()
		return
	}
	items := make([]*adminv1.Record, 0, len(res.Items))
	for _, rec := range res.Items {
		items = append(items, recordToProto(rec, mi))
	}
	resp.Body = &adminv1.DataStudioResponse_RecordsPage{
		RecordsPage: &adminv1.PaginatedRecords{
			Items:          items,
			Page:           uint32(res.Page),
			PageSize:       uint32(res.PageSize),
			Total:          res.Total,
			TotalEstimated: res.IsEstimated,
			HasMore:        res.HasMore,
		},
	}
}

func (h *Handler) handleGetRecord(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.GetRecordRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := h.authorize(op, mi.Name, "retrieve"); err != nil {
		resp.Error = err.Error()
		return
	}
	rec, err := h.getOwned(ctx, st, scopeFor(op, mi), req.GetId())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Record{Record: recordToProto(rec, mi)}
}

// getOwned reads one record and, for a scoped operator, confirms it is the
// tenant's: a row of another tenant is "not found", never "forbidden" — the
// scope hides what it does not own.
func (h *Handler) getOwned(ctx context.Context, st datasource.RecordStore, scope tenantScope, id string) (datasource.Record, error) {
	rec, err := st.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("not found: record %q", id)
	}
	if scope.Enforced() && !scope.owns(rec) {
		return nil, fmt.Errorf("not found: record %q", id)
	}
	return rec, nil
}

// =============================================================================
// create / update / delete / bulk
// =============================================================================

func (h *Handler) handleCreateRecord(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.CreateRecordRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := h.authorize(op, mi.Name, "create"); err != nil {
		resp.Error = err.Error()
		return
	}
	rec, err := recordFromProto(req.GetRecord())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	// A scoped operator creates in its tenant and nowhere else: the tenant
	// field is stamped, and a record that names another tenant is refused
	// rather than moved.
	if scope := scopeFor(op, mi); scope.Enforced() {
		if v, ok := lookup(rec, scope.Field.Name, scope.Field.Column); ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != scope.Tenant {
			resp.Error = fmt.Sprintf("permission denied: the record names tenant %q, the operator is scoped to %q", fmt.Sprint(v), scope.Tenant)
			return
		}
		delete(rec, scope.Field.Column)
		rec[scope.Field.Name] = scope.Tenant
	}
	created, err := st.Create(ctx, rec)
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if created == nil {
		created = rec
	}
	resp.Body = &adminv1.DataStudioResponse_Record{Record: recordToProto(created, mi)}
}

func (h *Handler) handleUpdateRecord(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.UpdateRecordRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := h.authorize(op, mi.Name, "update"); err != nil {
		resp.Error = err.Error()
		return
	}
	scope := scopeFor(op, mi)
	if _, err := h.getOwned(ctx, st, scope, req.GetId()); err != nil {
		resp.Error = err.Error()
		return
	}
	rec, err := recordFromProto(req.GetRecord())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if scope.Enforced() {
		// The tenant is not a field an update may move a row across.
		if v, ok := lookup(rec, scope.Field.Name, scope.Field.Column); ok && fmt.Sprint(v) != scope.Tenant {
			resp.Error = fmt.Sprintf("permission denied: an update may not move the record to tenant %q", fmt.Sprint(v))
			return
		}
		delete(rec, scope.Field.Column)
		delete(rec, scope.Field.Name)
	}
	if err := st.Update(ctx, req.GetId(), rec); err != nil {
		resp.Error = err.Error()
		return
	}
	updated, err := st.Get(ctx, req.GetId())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_Record{Record: recordToProto(updated, mi)}
}

func (h *Handler) handleDeleteRecord(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.DeleteRecordRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	if err := h.authorize(op, mi.Name, "delete"); err != nil {
		resp.Error = err.Error()
		return
	}
	if scope := scopeFor(op, mi); scope.Enforced() {
		if _, err := h.getOwned(ctx, st, scope, req.GetId()); err != nil {
			resp.Error = err.Error()
			return
		}
	}
	if err := st.Delete(ctx, req.GetId()); err != nil {
		resp.Error = err.Error()
		return
	}
	resp.Body = &adminv1.DataStudioResponse_DeleteRecord{
		DeleteRecord: &adminv1.DeleteRecordResponse{Deleted: true},
	}
}

func (h *Handler) handleBulkAction(ctx context.Context, resp *adminv1.DataStudioResponse, op *adminv1.OperatorIdentity, req *adminv1.BulkActionRequest) {
	mi, st, err := h.storeFor(req.GetModelName(), req.GetDatabaseAlias())
	if err != nil {
		resp.Error = err.Error()
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.GetAction())) {
	case "delete":
		if err := h.authorize(op, mi.Name, "bulk_delete"); err != nil {
			resp.Error = err.Error()
			return
		}
		scope := scopeFor(op, mi)
		out := &adminv1.BulkActionResponse{}
		for _, id := range req.GetIds() {
			if scope.Enforced() {
				if _, err := h.getOwned(ctx, st, scope, id); err != nil {
					out.Failed++
					out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", id, err))
					continue
				}
			}
			if err := st.Delete(ctx, id); err != nil {
				out.Failed++
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", id, err))
				continue
			}
			out.Affected++
		}
		resp.Body = &adminv1.DataStudioResponse_BulkAction{BulkAction: out}
	default:
		resp.Error = fmt.Sprintf("admin agent: unsupported bulk action %q", req.GetAction())
	}
}

// =============================================================================
// helpers
// =============================================================================

func (h *Handler) aliasFor(requested string, mi datasource.ModelInfo) string {
	if a := strings.TrimSpace(requested); a != "" {
		return a
	}
	if mi.DatabaseAlias != "" {
		return mi.DatabaseAlias
	}
	return h.cfg.defaultAlias()
}

func (h *Handler) storeFor(modelName, alias string) (datasource.ModelInfo, datasource.RecordStore, error) {
	mi, ok := h.cfg.Source.Get(modelName)
	if !ok {
		return datasource.ModelInfo{}, nil, fmt.Errorf("not found: model %q is not registered", modelName)
	}
	st, err := h.cfg.Source.Store(mi.Name, h.aliasFor(alias, mi))
	if err != nil {
		return datasource.ModelInfo{}, nil, err
	}
	if st == nil {
		return datasource.ModelInfo{}, nil, errors.New("admin agent: the data source returned no store")
	}
	return mi, st, nil
}

func cloneFilters(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func modelToProto(m datasource.ModelInfo) *adminv1.ModelInfo {
	return &adminv1.ModelInfo{
		Name:          m.Name,
		Plural:        m.Plural,
		Table:         m.Table,
		DatabaseAlias: m.DatabaseAlias,
		PrimaryKey:    m.PrimaryKey,
		RecordCount:   -1,
	}
}

func fieldsToProto(in []datasource.FieldInfo) []*adminv1.ModelField {
	out := make([]*adminv1.ModelField, 0, len(in))
	for _, f := range in {
		out = append(out, &adminv1.ModelField{
			Name:         f.Name,
			Column:       f.Column,
			Label:        f.Label,
			GoType:       f.GoType,
			HtmlType:     f.HTMLType,
			IsPrimaryKey: f.IsPK,
			IsRequired:   f.IsRequired,
			IsReadonly:   f.IsReadOnly,
			IsInList:     f.IsList,
			IsSearchable: f.IsSearch,
			IsFilterable: f.IsFilter,
			IsExcluded:   f.IsExcluded,
			IsForeignKey: f.IsForeignKey,
			ForeignModel: f.ForeignModel,
			Choices:      choicesToProto(f.Choices),
		})
	}
	return out
}

func choicesToProto(in []datasource.Choice) []*adminv1.FieldChoice {
	if len(in) == 0 {
		return nil
	}
	out := make([]*adminv1.FieldChoice, 0, len(in))
	for _, c := range in {
		out = append(out, &adminv1.FieldChoice{Value: c.Value, Label: c.Label})
	}
	return out
}

// fold is the key a record value is matched by: case and underscores
// ignored, so the Nucleus adapter's "tenant_id" (its JSON key) and the
// wire's "TenantID" (the Go field name the fleet SPA indexes by) meet.
func fold(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}

// lookup finds a value in a record under any of the given keys, exactly
// first and folded second.
func lookup(rec datasource.Record, keys ...string) (any, bool) {
	for _, k := range keys {
		if k == "" {
			continue
		}
		if v, ok := rec[k]; ok {
			return v, true
		}
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		fk := fold(k)
		for rk, v := range rec {
			if fold(rk) == fk {
				return v, true
			}
		}
	}
	return nil, false
}

// recordToProto renders a record the way the wire has always carried it:
// values_json keyed by the model's field NAMES, each value as its JSON
// text. The data source keys records its own way (the Nucleus adapter by
// JSON tag), so every field is looked up by name and column, folded; a
// field the record does not carry is left out, as before.
func recordToProto(rec datasource.Record, mi datasource.ModelInfo) *adminv1.Record {
	out := &adminv1.Record{ValuesJson: make(map[string]string, len(mi.Fields))}
	for _, f := range mi.Fields {
		if f.IsExcluded {
			continue
		}
		v, ok := lookup(rec, f.Name, f.Column)
		if !ok {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out.ValuesJson[f.Name] = string(raw)
	}
	return out
}

// recordFromProto decodes the wire's values_json (field name → JSON text)
// into a record keyed by those names; the data source resolves a field by
// its name as well as its column, so the keys pass through unchanged.
func recordFromProto(pr *adminv1.Record) (datasource.Record, error) {
	rec := make(datasource.Record, len(pr.GetValuesJson()))
	keys := make([]string, 0, len(pr.GetValuesJson()))
	for k := range pr.GetValuesJson() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw := pr.GetValuesJson()[k]
		var v any
		if strings.TrimSpace(raw) == "" {
			rec[k] = ""
			continue
		}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("admin agent: field %q: invalid JSON value %q", k, raw)
		}
		rec[k] = v
	}
	return rec, nil
}
