// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// Operators are the people who use the panel, as opposed to the policies that
// describe what they may do. Until this file existed the panel managed the
// second and not the first: roles and policies were editable from the UI while
// the accounts they applied to lived only in the `nucleus_admin_users` table
// and the `nucleus createuser` CLI. An operator who needed a colleague to have
// access had to leave the panel and open a shell on the server (OR-4).
//
// The surface is deliberately small — list, create, edit, set a password,
// deactivate, delete, and the two role verbs — and every mutation is audited
// like any other management action.
//
// Two invariants are enforced here rather than left to the UI, because the UI
// is not the only client:
//
//   - an operator cannot deactivate, delete or demote THEMSELVES: the refusal
//     is what stops someone from locking themselves out with one misclick;
//   - the last ACTIVE superuser cannot be deactivated, deleted or demoted by
//     anyone: the panel would still be there, and nobody could administer it.
//
// Deactivation is what the panel does instead of deletion where a person is
// concerned: `Authenticate` reads the row on every request and skips inactive
// operators, so the session dies on their next request — but the account, and
// with it the trail of what they did, is still there.

// operatorPasswordMinLength is the floor for a password set through the panel.
// It is deliberately the length of a short passphrase rather than a
// composition rule: the framework hashes with bcrypt at cost 12 and the panel
// locks out after repeated failures, so length is the property worth
// enforcing.
const operatorPasswordMinLength = 12

// operatorRecord is one operator as the panel serves them. The password hash
// is never in it — not even redacted: a hash that reaches a client is a hash
// someone can attack offline at their leisure.
type operatorRecord struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	IsSuperuser bool     `json:"is_superuser"`
	IsActive    bool     `json:"is_active"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Roles       []string `json:"roles"`
}

// operatorStore is the SQL side of operator management, over the same table
// and dialect DatabaseAdminAuth authenticates against.
type operatorStore struct {
	db     *sql.DB
	table  string
	system string
}

// operatorManager is what an admin auth provider implements when it owns a
// store of operators the panel may manage. DatabaseAdminAuth does; an
// application that delegates authentication elsewhere (ADR-004) does not, and
// the routes answer 501 rather than inventing an account store the
// application never asked for.
type operatorManager interface {
	OperatorStore() *operatorStore
}

// OperatorStore exposes the admin-users table to the panel's operator routes.
func (a *DatabaseAdminAuth) OperatorStore() *operatorStore {
	if a == nil || a.db == nil {
		return nil
	}
	return &operatorStore{db: a.db, table: a.tableName(), system: a.systemName()}
}

// binds returns n placeholders for this store's dialect, or an error naming
// the problem: without a dialect there is no portable way to bind a value,
// and operator management writes end-user input.
func (s *operatorStore) binds(n int) ([]string, error) {
	ph := bindPlaceholders(s.system, n)
	if ph == nil {
		return nil, fmt.Errorf("admin operators: unknown database dialect %q; operator management binds its values and cannot fall back to literals", s.system)
	}
	return ph, nil
}

const operatorSelectColumns = "id, username, email, is_superuser, is_active, created_at, updated_at"

func (s *operatorStore) scan(rows *sql.Rows) (operatorRecord, error) {
	var (
		rec                  operatorRecord
		superRaw, activeRaw  interface{}
		createdRaw, updatedR interface{}
	)
	if err := rows.Scan(&rec.ID, &rec.Username, &rec.Email, &superRaw, &activeRaw, &createdRaw, &updatedR); err != nil {
		return operatorRecord{}, err
	}
	rec.ID = strings.TrimSpace(rec.ID)
	rec.Username = strings.TrimSpace(rec.Username)
	rec.Email = strings.TrimSpace(rec.Email)
	rec.IsSuperuser = parseAdminSuperuserValue(superRaw)
	rec.IsActive = parseAdminSuperuserValue(activeRaw)
	rec.CreatedAt = timestampText(createdRaw)
	rec.UpdatedAt = timestampText(updatedR)
	rec.Roles = []string{}
	return rec, nil
}

// timestampText renders whatever the driver returns for the timestamp
// columns. They are written as RFC3339 strings, but a driver is free to hand
// back []byte (MySQL) or a time.Time (a column someone migrated to a real
// timestamp type), and a panel that shows an empty date because of that is a
// panel that looks broken.
func timestampText(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (s *operatorStore) list(ctx context.Context) ([]operatorRecord, error) {
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY username", operatorSelectColumns, s.table)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]operatorRecord, 0, 8)
	for rows.Next() {
		rec, err := s.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan operator: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operators: %w", err)
	}
	return out, nil
}

// errOperatorNotFound is what every read returns for an id nobody has, so the
// handlers answer 404 instead of leaking the difference between "no such row"
// and "the query failed".
var errOperatorNotFound = errors.New("operator not found")

func (s *operatorStore) get(ctx context.Context, id string) (operatorRecord, error) {
	ph, err := s.binds(1)
	if err != nil {
		return operatorRecord{}, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", operatorSelectColumns, s.table, ph[0])
	rows, err := s.db.QueryContext(ctx, query, id)
	if err != nil {
		return operatorRecord{}, fmt.Errorf("get operator: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return operatorRecord{}, fmt.Errorf("get operator: %w", err)
		}
		return operatorRecord{}, errOperatorNotFound
	}
	return s.scan(rows)
}

func (s *operatorStore) create(ctx context.Context, rec operatorRecord, passwordHash string) (operatorRecord, error) {
	ph, err := s.binds(8)
	if err != nil {
		return operatorRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec.ID = newBootstrapAdminUserID()
	rec.CreatedAt, rec.UpdatedAt = now, now
	rec.IsActive = true

	stmt := fmt.Sprintf("INSERT INTO %s %s VALUES (%s)", s.table, adminUsersInsertColumns, strings.Join(ph, ", "))
	if _, err := s.db.ExecContext(ctx, stmt,
		rec.ID, rec.Username, rec.Email, passwordHash, boolToInt(rec.IsSuperuser), 1, now, now); err != nil {
		return operatorRecord{}, err
	}
	return rec, nil
}

func (s *operatorStore) updateProfile(ctx context.Context, id, email string, superuser bool) error {
	ph, err := s.binds(4)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf("UPDATE %s SET email = %s, is_superuser = %s, updated_at = %s WHERE id = %s",
		s.table, ph[0], ph[1], ph[2], ph[3])
	return s.exec(ctx, stmt, email, boolToInt(superuser), time.Now().UTC().Format(time.RFC3339), id)
}

func (s *operatorStore) setPassword(ctx context.Context, id, hash string) error {
	ph, err := s.binds(3)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf("UPDATE %s SET password_hash = %s, updated_at = %s WHERE id = %s",
		s.table, ph[0], ph[1], ph[2])
	return s.exec(ctx, stmt, hash, time.Now().UTC().Format(time.RFC3339), id)
}

func (s *operatorStore) setActive(ctx context.Context, id string, active bool) error {
	ph, err := s.binds(3)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf("UPDATE %s SET is_active = %s, updated_at = %s WHERE id = %s",
		s.table, ph[0], ph[1], ph[2])
	return s.exec(ctx, stmt, boolToInt(active), time.Now().UTC().Format(time.RFC3339), id)
}

func (s *operatorStore) delete(ctx context.Context, id string) error {
	ph, err := s.binds(1)
	if err != nil {
		return err
	}
	return s.exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = %s", s.table, ph[0]), id)
}

// exec runs a statement that must touch exactly one row and turns "no rows"
// into errOperatorNotFound — the alternative is a 200 for an id that does not
// exist, which reads as success.
func (s *operatorStore) exec(ctx context.Context, stmt string, args ...any) error {
	res, err := s.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	// RowsAffected is optional in the driver contract; when it is not
	// available the statement is taken at its word rather than failing.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errOperatorNotFound
	}
	return nil
}

// activeSuperusers counts the operators who could still administer the panel,
// optionally excluding one id — the one about to be deactivated, deleted or
// demoted.
func (s *operatorStore) activeSuperusers(ctx context.Context, excludeID string) (int, error) {
	ph, err := s.binds(1)
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE is_superuser = 1 AND is_active = 1 AND id <> %s", s.table, ph[0])
	var n int
	if err := s.db.QueryRowContext(ctx, query, excludeID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active superusers: %w", err)
	}
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- handlers ---------------------------------------------------------------

// operatorStoreFor resolves the store, or the error the route answers with
// when this panel's auth provider does not own one.
func (p *Panel) operatorStoreFor() (*operatorStore, error) {
	mgr, ok := p.config.Auth.(operatorManager)
	if !ok || mgr == nil {
		return nil, &gferrors.DomainError{
			Code:       "NOT_IMPLEMENTED",
			Message:    "this panel's authentication provider does not manage operators; they are created where that provider keeps them",
			StatusCode: http.StatusNotImplemented,
		}
	}
	store := mgr.OperatorStore()
	if store == nil {
		// The provider names the contract and has no store behind it — a
		// panel whose admin authentication is not backed by a database.
		// Same answer as a provider that does not implement it at all:
		// the capability is absent here, and that is not the caller's
		// mistake to correct.
		return nil, &gferrors.DomainError{
			Code:       "NOT_IMPLEMENTED",
			Message:    "this panel's admin authentication is not backed by a database, so it keeps no operators to manage",
			StatusCode: http.StatusNotImplemented,
		}
	}
	return store, nil
}

// withRoles fills each record's Roles from the policy engine, so one screen
// answers both questions a person asks about an operator: who they are, and
// what they may do.
func (p *Panel) withRoles(records []operatorRecord) []operatorRecord {
	if p.rbac == nil {
		return records
	}
	for i := range records {
		roles := p.rbac.GetRoles(records[i].Username)
		if roles == nil {
			roles = []string{}
		}
		records[i].Roles = roles
	}
	return records
}

func (p *Panel) handleListOperators(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "operators_list"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	records, err := store.list(c.Request.Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"operators": p.withRoles(records),
		"total":     len(records),
	})
}

func (p *Panel) handleGetOperator(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "operators_list"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	rec, err := store.get(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		return operatorError(err)
	}
	return c.JSON(http.StatusOK, p.withRoles([]operatorRecord{rec})[0])
}

func (p *Panel) handleCreateOperator(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}

	var req struct {
		Username    string   `json:"username"`
		Email       string   `json:"email"`
		Password    string   `json:"password"`
		IsSuperuser bool     `json:"is_superuser"`
		Roles       []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Email == "" {
		return gferrors.BadRequest("username and email are required")
	}
	if err := validateOperatorPassword(req.Password); err != nil {
		return err
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return fmt.Errorf("hash operator password: %w", err)
	}

	rec, err := store.create(r.Context(), operatorRecord{
		Username:    req.Username,
		Email:       req.Email,
		IsSuperuser: req.IsSuperuser,
	}, hash)
	if err != nil {
		if isBootstrapDuplicateError(err) {
			return &gferrors.DomainError{
				Code:       "CONFLICT",
				Message:    fmt.Sprintf("an operator with that username or email already exists (%q, %q)", req.Username, req.Email),
				StatusCode: http.StatusConflict,
			}
		}
		return err
	}

	granted := []string{}
	if p.rbac != nil {
		for _, role := range req.Roles {
			role = strings.TrimSpace(role)
			if role == "" {
				continue
			}
			if err := p.rbac.AddRole(rec.Username, role); err != nil {
				return err
			}
			granted = append(granted, role)
		}
	}
	rec.Roles = granted

	p.recordAuditEntry(r, AuditEntry{
		Action:    "operator.create",
		ModelName: "operator",
		RecordID:  rec.ID,
		NewValue: map[string]any{
			"username": rec.Username, "email": rec.Email,
			"is_superuser": rec.IsSuperuser, "roles": granted,
		},
	})
	return c.JSON(http.StatusCreated, rec)
}

func (p *Panel) handleUpdateOperator(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Param("id"))
	before, err := store.get(r.Context(), id)
	if err != nil {
		return operatorError(err)
	}

	var req struct {
		Email       *string `json:"email"`
		IsSuperuser *bool   `json:"is_superuser"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	email := before.Email
	if req.Email != nil {
		if email = strings.TrimSpace(*req.Email); email == "" {
			return gferrors.BadRequest("email cannot be empty")
		}
	}
	superuser := before.IsSuperuser
	if req.IsSuperuser != nil {
		superuser = *req.IsSuperuser
	}

	if before.IsSuperuser && !superuser {
		if err := p.guardLastSuperuser(c, before, "demoted"); err != nil {
			return err
		}
	}

	if err := store.updateProfile(r.Context(), id, email, superuser); err != nil {
		if isBootstrapDuplicateError(err) {
			return &gferrors.DomainError{
				Code:       "CONFLICT",
				Message:    fmt.Sprintf("another operator already uses the email %q", email),
				StatusCode: http.StatusConflict,
			}
		}
		return operatorError(err)
	}

	after, err := store.get(r.Context(), id)
	if err != nil {
		return operatorError(err)
	}
	p.recordAuditEntry(r, AuditEntry{
		Action:    "operator.update",
		ModelName: "operator",
		RecordID:  id,
		OldValue:  map[string]any{"email": before.Email, "is_superuser": before.IsSuperuser},
		NewValue:  map[string]any{"email": after.Email, "is_superuser": after.IsSuperuser},
	})
	return c.JSON(http.StatusOK, p.withRoles([]operatorRecord{after})[0])
}

func (p *Panel) handleSetOperatorPassword(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Param("id"))

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	if err := validateOperatorPassword(req.Password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return fmt.Errorf("hash operator password: %w", err)
	}
	if err := store.setPassword(r.Context(), id, hash); err != nil {
		return operatorError(err)
	}

	// The password, and nothing about it, reaches the trail: an audit entry
	// is read by every operator who can list it.
	p.recordAuditEntry(r, AuditEntry{
		Action:    "operator.password.set",
		ModelName: "operator",
		RecordID:  id,
		NewValue:  map[string]any{"password_changed": true},
	})
	return c.JSON(http.StatusOK, map[string]any{"id": id, "password_changed": true})
}

// handleSetOperatorActive serves both /disable and /enable: same guards, same
// audit shape, one bit of difference.
func (p *Panel) handleSetOperatorActive(active bool) router.Handler {
	return func(c *router.Context) error {
		r := c.Request
		if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
			return err
		}
		store, err := p.operatorStoreFor()
		if err != nil {
			return err
		}
		id := strings.TrimSpace(c.Param("id"))
		before, err := store.get(r.Context(), id)
		if err != nil {
			return operatorError(err)
		}
		if !active {
			if err := p.guardSelf(c, before, "deactivate"); err != nil {
				return err
			}
			if before.IsSuperuser {
				if err := p.guardLastSuperuser(c, before, "deactivated"); err != nil {
					return err
				}
			}
		}
		if err := store.setActive(r.Context(), id, active); err != nil {
			return operatorError(err)
		}

		action := "operator.disable"
		if active {
			action = "operator.enable"
		}
		p.recordAuditEntry(r, AuditEntry{
			Action:    action,
			ModelName: "operator",
			RecordID:  id,
			OldValue:  map[string]any{"is_active": before.IsActive},
			NewValue:  map[string]any{"is_active": active, "username": before.Username},
		})
		return c.JSON(http.StatusOK, map[string]any{"id": id, "is_active": active})
	}
}

func (p *Panel) handleDeleteOperator(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
		return err
	}
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Param("id"))
	before, err := store.get(r.Context(), id)
	if err != nil {
		return operatorError(err)
	}
	if err := p.guardSelf(c, before, "delete"); err != nil {
		return err
	}
	if before.IsSuperuser && before.IsActive {
		if err := p.guardLastSuperuser(c, before, "deleted"); err != nil {
			return err
		}
	}
	if err := store.delete(r.Context(), id); err != nil {
		return operatorError(err)
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "operator.delete",
		ModelName: "operator",
		RecordID:  id,
		OldValue: map[string]any{
			"username": before.Username, "email": before.Email,
			"is_superuser": before.IsSuperuser, "is_active": before.IsActive,
		},
	})
	return c.JSON(http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// handleSetOperatorRole serves POST (grant) and DELETE (revoke) on the same
// path. Roles live in the policy engine, keyed by username — the same subject
// the policy rows use — so this is a convenience over /api/rbac/roles that
// keeps the operator screen from having to know that.
func (p *Panel) handleSetOperatorRole(grant bool) router.Handler {
	return func(c *router.Context) error {
		r := c.Request
		if err := p.authorizeAction(c, "*", "operators_manage"); err != nil {
			return err
		}
		if p.rbac == nil {
			return gferrors.BadRequest("RBAC enforcer not configured")
		}
		store, err := p.operatorStoreFor()
		if err != nil {
			return err
		}
		id := strings.TrimSpace(c.Param("id"))
		rec, err := store.get(r.Context(), id)
		if err != nil {
			return operatorError(err)
		}

		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return gferrors.BadRequest("invalid JSON")
		}
		role := strings.TrimSpace(req.Role)
		if role == "" {
			return gferrors.BadRequest("role is required")
		}

		action := "operator.role.revoke"
		if grant {
			if err := p.rbac.AddRole(rec.Username, role); err != nil {
				return err
			}
			action = "operator.role.grant"
		} else if err := p.rbac.RemoveRole(rec.Username, role); err != nil {
			return err
		}

		p.recordAuditEntry(r, AuditEntry{
			Action:    action,
			ModelName: "operator",
			RecordID:  id,
			NewValue:  map[string]any{"username": rec.Username, "role": role},
		})
		return c.JSON(http.StatusOK, map[string]any{
			"id": id, "username": rec.Username, "role": role, "granted": grant,
		})
	}
}

// guardSelf refuses the operations a person should not be able to perform on
// their own account through the panel. Locking yourself out is one click, and
// undoing it needs a shell on the server — which is the very thing this
// surface exists to make unnecessary.
func (p *Panel) guardSelf(c *router.Context, target operatorRecord, verb string) error {
	actor, err := p.authenticatedUser(c.Request)
	if err != nil || actor == nil {
		return nil
	}
	if strings.TrimSpace(actor.ID) != target.ID {
		return nil
	}
	return &gferrors.DomainError{
		Code:       "CONFLICT",
		Message:    fmt.Sprintf("you cannot %s your own operator account; ask another superuser to do it", verb),
		StatusCode: http.StatusConflict,
	}
}

// guardLastSuperuser refuses to leave the panel with no one who can
// administer it.
func (p *Panel) guardLastSuperuser(c *router.Context, target operatorRecord, verb string) error {
	store, err := p.operatorStoreFor()
	if err != nil {
		return err
	}
	others, err := store.activeSuperusers(c.Request.Context(), target.ID)
	if err != nil {
		return err
	}
	if others > 0 {
		return nil
	}
	return &gferrors.DomainError{
		Code:       "CONFLICT",
		Message:    fmt.Sprintf("%q is the last active superuser and cannot be %s: promote another operator first", target.Username, verb),
		StatusCode: http.StatusConflict,
	}
}

func validateOperatorPassword(password string) error {
	if len([]rune(password)) < operatorPasswordMinLength {
		return gferrors.BadRequest(fmt.Sprintf("password must be at least %d characters", operatorPasswordMinLength))
	}
	return nil
}

// operatorError maps the store's not-found to a 404 and leaves everything
// else as it is.
func operatorError(err error) error {
	if errors.Is(err, errOperatorNotFound) {
		return gferrors.NotFound("operator", "")
	}
	return err
}
