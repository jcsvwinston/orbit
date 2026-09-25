package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/server/auth"
	"github.com/jcsvwinston/orbit/server/nodes"
	"github.com/jcsvwinston/orbit/server/routing"
	"github.com/jcsvwinston/orbit/server/store"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// DataStudioService implements adminv1connect.DataStudioServiceHandler.
//
// Every method routes the call to a connected agent that knows the
// requested model, sends a DataStudioRequest down its bidi stream, and
// blocks on the matching DataStudioResponse for at most Timeout.
type DataStudioService struct {
	state   *State
	Timeout time.Duration

	// allowedModels is the lower-cased mutation allowlist
	// (Config.DataStudioAllowedModels); allowAllModels is the "*" entry.
	// Deny-by-default: both zero → every mutation is refused.
	allowedModels  map[string]struct{}
	allowAllModels bool
}

// NewDataStudioService constructs the handler. timeout <= 0 defaults
// to 10s. allowedModels is the per-model mutation allowlist (see
// Config.DataStudioAllowedModels): empty refuses every mutation, the
// single entry "*" allows them on every model.
func NewDataStudioService(state *State, timeout time.Duration, allowedModels []string) *DataStudioService {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	s := &DataStudioService{
		state:         state,
		Timeout:       timeout,
		allowedModels: make(map[string]struct{}, len(allowedModels)),
	}
	for _, m := range allowedModels {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if m == "*" {
			s.allowAllModels = true
			continue
		}
		s.allowedModels[m] = struct{}{}
	}
	return s
}

// ListModels: returns the union of every connected agent's registered
// model set. When include_counts is true the call is routed to ONE
// agent (the first connected one) so the counts are coherent.
func (s *DataStudioService) ListModels(ctx context.Context, req *connect.Request[adminv1.ListModelsRequest]) (*connect.Response[adminv1.ListModelsResponse], error) {
	body := req.Msg
	if !body.GetIncludeCounts() && body.GetNodeId() == "" {
		// Fast path: synthesize the response from the registry without
		// hitting any agent. The UI uses this to populate its sidebar.
		names := s.state.Nodes.AggregateModels()
		out := &adminv1.ListModelsResponse{Models: make([]*adminv1.ModelInfo, 0, len(names))}
		for _, n := range names {
			out.Models = append(out.Models, &adminv1.ModelInfo{
				Name:        n,
				RecordCount: -1,
			})
		}
		return connect.NewResponse(out), nil
	}

	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_ListModels{ListModels: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), "", wrapped)
	if err != nil {
		return nil, err
	}
	if list := resp.GetListModels(); list != nil {
		// The wire says which agent answered; with more than one node the
		// UI cannot tell otherwise, and the agent does not name itself.
		if list.GetNodeId() == "" {
			list.NodeId = node
		}
		return connect.NewResponse(list), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty list_models response"))
}

func (s *DataStudioService) GetSchema(ctx context.Context, req *connect.Request[adminv1.GetSchemaRequest]) (*connect.Response[adminv1.ModelSchema], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_GetSchema{GetSchema: body}}
	resp, _, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if schema := resp.GetSchema(); schema != nil {
		return connect.NewResponse(schema), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty get_schema response"))
}

func (s *DataStudioService) ListRecords(ctx context.Context, req *connect.Request[adminv1.ListRecordsRequest]) (*connect.Response[adminv1.PaginatedRecords], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_ListRecords{ListRecords: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if page := resp.GetRecordsPage(); page != nil {
		if page.GetNodeId() == "" {
			page.NodeId = node
		}
		return connect.NewResponse(page), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty list_records response"))
}

func (s *DataStudioService) GetRecord(ctx context.Context, req *connect.Request[adminv1.GetRecordRequest]) (*connect.Response[adminv1.Record], error) {
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_GetRecord{GetRecord: body}}
	resp, _, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeNotFound, errors.New("admin server: record not found"))
}

func (s *DataStudioService) CreateRecord(ctx context.Context, req *connect.Request[adminv1.CreateRecordRequest]) (*connect.Response[adminv1.Record], error) {
	if err := s.requireWrite(ctx, req.Msg.GetModelName()); err != nil {
		return nil, err
	}
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_CreateRecord{CreateRecord: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		s.audit(ctx, "datastudio.create", auditTarget(body.GetModelName(), recordID(rec, ""), body.GetDatabaseAlias()), node,
			auditSides{After: recordJSON(rec)})
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty create response"))
}

func (s *DataStudioService) UpdateRecord(ctx context.Context, req *connect.Request[adminv1.UpdateRecordRequest]) (*connect.Response[adminv1.Record], error) {
	if err := s.requireWrite(ctx, req.Msg.GetModelName()); err != nil {
		return nil, err
	}
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_UpdateRecord{UpdateRecord: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if rec := resp.GetRecord(); rec != nil {
		s.audit(ctx, "datastudio.update", auditTarget(body.GetModelName(), body.GetId(), body.GetDatabaseAlias()), node,
			auditSides{Before: previousJSON(resp), After: recordJSON(rec)})
		return connect.NewResponse(rec), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty update response"))
}

func (s *DataStudioService) DeleteRecord(ctx context.Context, req *connect.Request[adminv1.DeleteRecordRequest]) (*connect.Response[adminv1.DeleteRecordResponse], error) {
	if err := s.requireWrite(ctx, req.Msg.GetModelName()); err != nil {
		return nil, err
	}
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_DeleteRecord{DeleteRecord: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if del := resp.GetDeleteRecord(); del != nil {
		s.audit(ctx, "datastudio.delete", auditTarget(body.GetModelName(), body.GetId(), body.GetDatabaseAlias()), node,
			auditSides{Before: previousJSON(resp)})
		return connect.NewResponse(del), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty delete response"))
}

func (s *DataStudioService) BulkAction(ctx context.Context, req *connect.Request[adminv1.BulkActionRequest]) (*connect.Response[adminv1.BulkActionResponse], error) {
	if err := s.requireWrite(ctx, req.Msg.GetModelName()); err != nil {
		return nil, err
	}
	body := req.Msg
	wrapped := &adminv1.DataStudioRequest{Body: &adminv1.DataStudioRequest_BulkAction{BulkAction: body}}
	resp, node, err := s.dispatch(ctx, body.GetNodeId(), body.GetModelName(), wrapped)
	if err != nil {
		return nil, err
	}
	if bulk := resp.GetBulkAction(); bulk != nil {
		s.audit(ctx, "datastudio.bulk."+body.GetAction(),
			fmt.Sprintf("%s ×%d (%s)", body.GetModelName(), len(body.GetIds()), aliasOrDefault(body.GetDatabaseAlias())), node,
			auditSides{Before: previousJSON(resp)})
		return connect.NewResponse(bulk), nil
	}
	return nil, connect.NewError(connect.CodeUnknown, errors.New("admin server: empty bulk response"))
}

// dispatch picks an agent, allocates a request_id, and sends the
// pre-built request over the agent's bidi stream. Blocks on the
// matching DataStudioResponse for at most s.Timeout.
// operatorOnTheWire is the identity the UI auth chain resolved, as the
// agent receives it (ADR-002): subject, email, role, whether the operator
// is read-only and the tenant it is scoped to. Nil when the request carries
// no identity, which the agent treats as "no operator" — its behaviour
// before the field existed.
func operatorOnTheWire(ctx context.Context) *adminv1.OperatorIdentity {
	id := auth.IdentityFromContext(ctx)
	if strings.TrimSpace(id.Subject) == "" {
		return nil
	}
	return &adminv1.OperatorIdentity{
		Subject:  id.Subject,
		Email:    id.Email,
		Role:     id.Role,
		ReadOnly: id.ReadOnly,
		Tenant:   id.Tenant,
	}
}

// agentErrorCode maps the agent's error text onto a Connect code. Two
// prefixes are a wire convention with the agent's Data Studio handler:
// "permission denied:" and "not found:". Anything else is Unknown, as
// before.
func agentErrorCode(msg string) connect.Code {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.HasPrefix(lower, "permission denied"):
		return connect.CodePermissionDenied
	case strings.HasPrefix(lower, "not found"):
		return connect.CodeNotFound
	}
	return connect.CodeUnknown
}

func (s *DataStudioService) dispatch(ctx context.Context, nodeID, modelName string, req *adminv1.DataStudioRequest) (*adminv1.DataStudioResponse, string, error) {
	if s == nil || s.state == nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}

	entry, ok := s.pickAgent(nodeID, modelName)
	if ok && entry.Remote() {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("admin server: node %q is connected to server %q; open the Data Studio there", entry.NodeID, entry.Info.Via))
	}
	if !ok {
		if nodeID != "" {
			return nil, "", connect.NewError(connect.CodeNotFound,
				fmt.Errorf("admin server: node %q is not connected", nodeID))
		}
		if modelName != "" {
			return nil, "", connect.NewError(connect.CodeNotFound,
				fmt.Errorf("admin server: no connected agent registered model %q", modelName))
		}
		return nil, "", connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: no agents connected"))
	}

	id, ch, cancel, err := s.state.DataStudio.Begin()
	if err != nil {
		return nil, "", connect.NewError(connect.CodeResourceExhausted, err)
	}
	req.RequestId = id
	req.Operator = operatorOnTheWire(ctx)

	frame := &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_DataStudio{DataStudio: req},
			},
		},
	}
	if !nodes.TryEnqueue(entry, frame) {
		cancel()
		return nil, "", connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: agent send buffer full or stream closing"))
	}

	resp, err := routing.WaitDataStudio(ch, cancel, s.Timeout)
	outcome := "ok"
	defer func() {
		if c := s.state.Counters; c != nil {
			c.DataStudioRequestsTotal.WithLabelValues(outcome).Inc()
		}
	}()
	if err != nil {
		outcome = "error"
		return nil, "", connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if resp == nil {
		outcome = "error"
		return nil, "", connect.NewError(connect.CodeUnavailable, errors.New("admin server: empty data studio response"))
	}
	if resp.Error != "" {
		outcome = "error"
		return nil, "", connect.NewError(agentErrorCode(resp.Error), errors.New(resp.Error))
	}
	return resp, entry.NodeID, nil
}

// pickAgent returns the entry that should serve the request:
//   - If nodeID is set, that exact node (error when not connected).
//   - Else if modelName is set, any connected agent that has the model.
//   - Else any connected agent (used by include_counts=false ListModels).
func (s *DataStudioService) pickAgent(nodeID, modelName string) (*nodes.Entry, bool) {
	if id := strings.TrimSpace(nodeID); id != "" {
		return s.state.Nodes.Lookup(id)
	}
	if m := strings.TrimSpace(modelName); m != "" {
		return s.state.Nodes.AnyWithModel(m)
	}
	// No constraint: first entry wins.
	var pick *nodes.Entry
	s.state.Nodes.ForEach(func(e *nodes.Entry) {
		if pick == nil {
			pick = e
		}
	})
	return pick, pick != nil
}

// Compile-time assertion.
var _ adminv1connect.DataStudioServiceHandler = (*DataStudioService)(nil)

// requireWrite refuses the call when the authenticated operator is
// read-only (viewer role via the trusted proxy, or a server running
// with Config.UIReadOnly), or when the target model is not on the
// mutation allowlist (Config.DataStudioAllowedModels, deny-by-default).
// The allowlist exists because the agent executes the mutation on its
// database WITHOUT the application's per-model RBAC or tenant filtering
// — no operator identity crosses the stream — so which models the fleet
// plane may write is an explicit server-side decision. Reads are never
// gated here.
func (s *DataStudioService) requireWrite(ctx context.Context, modelName string) error {
	if auth.IdentityFromContext(ctx).ReadOnly {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("admin server: operator is read-only; data studio mutations are disabled"))
	}
	if s.allowAllModels {
		return nil
	}
	if _, ok := s.allowedModels[strings.ToLower(strings.TrimSpace(modelName))]; ok {
		return nil
	}
	if len(s.allowedModels) == 0 {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("admin server: data studio mutations are disabled by default; "+
				"allow specific models with --datastudio-allowed-models (Config.DataStudioAllowedModels), or \"*\" for all"))
	}
	return connect.NewError(connect.CodePermissionDenied,
		fmt.Errorf("admin server: model %q is not on the data studio mutation allowlist (--datastudio-allowed-models)", modelName))
}

// auditSides is what changed: the record's values before and after the
// action as JSON, each side empty when the action has none.
type auditSides struct {
	Before string
	After  string
}

// maxAuditSideBytes bounds one side of an audit entry. The ring is
// in-memory and holds thousands of entries; a record with a large text or
// blob column must not turn it into a copy of the table. A side over the
// bound is replaced by a JSON object that says so and how large it was.
const maxAuditSideBytes = 64 << 10

// audit records a fleet-plane action in the server's audit ring,
// attributed to the operator resolved by the UI auth chain, with what
// changed.
func (s *DataStudioService) audit(ctx context.Context, action, target, nodeID string, sides auditSides) {
	if s == nil || s.state == nil || s.state.Audit == nil {
		return
	}
	actor := auth.IdentityFromContext(ctx).Subject
	if actor == "" {
		actor = "unknown"
	}
	entry := routing.AuditEntry{
		Actor:  actor,
		Action: action,
		Target: target,
		NodeID: nodeID,
		Before: boundSide(sides.Before),
		After:  boundSide(sides.After),
	}
	s.state.Audit.Append(entry)
	s.state.Store.AppendAudit(store.AuditEntry{
		Actor: entry.Actor, Action: entry.Action, Target: entry.Target, NodeID: entry.NodeID,
		Before: entry.Before, After: entry.After,
	})
}

func boundSide(side string) string {
	if len(side) <= maxAuditSideBytes {
		return side
	}
	return fmt.Sprintf(`{"truncated":true,"bytes":%d}`, len(side))
}

// recordJSON renders a wire record as one JSON object: values_json maps
// each field name to JSON text already, so the object is assembled from
// those fragments as they are. Keys are sorted, so two renderings of the
// same record compare equal. A value that is not valid JSON is kept as a
// JSON string, never dropped: the audit must not lose a field because an
// agent encoded it oddly.
func recordJSON(rec *adminv1.Record) string {
	if rec == nil {
		return ""
	}
	values := rec.GetValuesJson()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		raw := values[k]
		if json.Valid([]byte(raw)) {
			b.WriteString(raw)
		} else {
			quoted, _ := json.Marshal(raw)
			b.Write(quoted)
		}
	}
	b.WriteByte('}')
	return b.String()
}

// previousJSON renders the records an agent returned as they were before
// the action: one record is an object, several (a bulk action) a JSON
// array, none — an agent that predates the field — the empty string.
func previousJSON(resp *adminv1.DataStudioResponse) string {
	prev := resp.GetPrevious()
	switch {
	case len(prev) == 0:
		return ""
	case len(prev) == 1 && resp.GetBulkAction() == nil:
		return recordJSON(prev[0])
	}
	parts := make([]string, 0, len(prev))
	for _, r := range prev {
		parts = append(parts, recordJSON(r))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// recordID extracts the record's id from its JSON value map ("" when
// the agent's response carries none — the target stays model-level).
//
// The agent keys values by Go FIELD name (datastudio.recordFromEntity),
// so a model.BaseModel primary key arrives as "ID", not "id"; an exact
// "id" lookup left every create audit entry without an id. The lookup is
// case-insensitive and, when the caller knows the model's primary key
// (ListModels' primary_key), that name is tried first.
func recordID(rec *adminv1.Record, primaryKey string) string {
	if rec == nil {
		return ""
	}
	values := rec.GetValuesJson()
	candidates := []string{"id"}
	if pk := strings.TrimSpace(primaryKey); pk != "" {
		candidates = append([]string{pk}, candidates...)
	}
	for _, want := range candidates {
		for key, raw := range values {
			if strings.EqualFold(key, want) {
				return strings.Trim(raw, `"`)
			}
		}
	}
	return ""
}

func auditTarget(model, id, alias string) string {
	if id == "" {
		return fmt.Sprintf("%s (%s)", model, aliasOrDefault(alias))
	}
	return fmt.Sprintf("%s #%s (%s)", model, id, aliasOrDefault(alias))
}

func aliasOrDefault(alias string) string {
	if strings.TrimSpace(alias) == "" {
		return "default"
	}
	return alias
}
