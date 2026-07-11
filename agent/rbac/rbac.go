// Package rbac is the agent-side handler for RbacRequest frames sent by
// the admin server. The admin server has no access to the application's
// authorizer; it routes the UI's Access control reads to a connected
// agent over the existing bidi stream. The agent snapshots the Casbin
// roles and policies locally (read-only — the app's authorizer stays the
// single writer) and sends an RbacResponse back.
//
// Construction is opt-in: pass a non-nil PolicySource (the framework's
// *authz.Enforcer satisfies it) in the agent's Config. When the source is
// nil, the Handler is disabled and the agent answers RbacRequests with a
// canned "rbac not enabled on this agent" error response.
package rbac

import (
	"fmt"
	"sort"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// PolicySource is the read-only slice of the framework's authorizer the
// handler needs. *authz.Enforcer (nucleus) satisfies it.
type PolicySource interface {
	// GetPolicy returns the policy rows: {subject, object, action} with an
	// optional fourth effect column ("allow"/"deny") depending on model.
	GetPolicy() ([][]string, error)
	// GetGroupingPolicy returns the grouping rows: {subject, role}.
	GetGroupingPolicy() ([][]string, error)
	// GetAllRoles returns every role that appears in a grouping rule.
	GetAllRoles() ([]string, error)
}

// Handler answers RbacRequest frames. Goroutine-safe: it holds no state
// beyond the source, and each Dispatch snapshot is independent.
type Handler struct {
	src PolicySource
}

// New constructs a Handler. Returns nil when src is nil (caller treats
// this as "RBAC disabled on this agent").
func New(src PolicySource) *Handler {
	if src == nil {
		return nil
	}
	return &Handler{src: src}
}

// Dispatch executes the snapshot and returns the response. The response
// always carries the same RequestId; on failure, Error is non-empty and
// the lists are empty. Dispatch never returns nil.
func (h *Handler) Dispatch(req *adminv1.RbacRequest) *adminv1.RbacResponse {
	resp := &adminv1.RbacResponse{RequestId: req.GetRequestId()}
	if h == nil || h.src == nil {
		resp.Error = "admin agent: rbac is not enabled on this node"
		return resp
	}

	policies, err := h.src.GetPolicy()
	if err != nil {
		resp.Error = fmt.Sprintf("admin agent: rbac policies: %v", err)
		return resp
	}
	grouping, err := h.src.GetGroupingPolicy()
	if err != nil {
		resp.Error = fmt.Sprintf("admin agent: rbac grouping: %v", err)
		return resp
	}
	roles, err := h.src.GetAllRoles()
	if err != nil {
		resp.Error = fmt.Sprintf("admin agent: rbac roles: %v", err)
		return resp
	}

	// Member counts: one per grouping rule {subject, role}.
	members := make(map[string]int32, len(roles))
	for _, g := range grouping {
		if len(g) >= 2 {
			members[g[1]]++
		}
	}
	sort.Strings(roles)
	resp.Roles = make([]*adminv1.RbacRole, 0, len(roles))
	for _, r := range roles {
		resp.Roles = append(resp.Roles, &adminv1.RbacRole{Name: r, Members: members[r]})
	}

	resp.Policies = make([]*adminv1.RbacPolicy, 0, len(policies))
	for _, p := range policies {
		if len(p) < 3 {
			continue
		}
		effect := "allow"
		if len(p) >= 4 && p[3] != "" {
			effect = p[3]
		}
		resp.Policies = append(resp.Policies, &adminv1.RbacPolicy{
			Subject: p[0],
			Object:  p[1],
			Action:  p[2],
			Effect:  effect,
		})
	}
	return resp
}
