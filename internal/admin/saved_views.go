// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The filter set an operator uses every morning.
//
// Filters lived in the URL and nowhere else: the operator who looks at
// "unpaid invoices over 90 days, oldest first" every day rebuilt it every day,
// or kept a bookmark that broke the moment the panel's query string changed.
//
// A saved view is a name, a model and the query string the grid was showing —
// stored as text on purpose. The panel does not parse it or validate it
// against today's schema: a view is a shortcut to a URL, and a query that
// stops making sense fails where any other query would, on the list endpoint,
// with the message that endpoint gives.
//
// Ownership: a view belongs to the operator who created it. A shared one is
// visible to everybody and editable by its owner (a superuser may edit any).
// Sharing is a flag rather than a permission of its own, because the thing
// being shared is a URL, and everything it can show is already gated by the
// list endpoint it points at.
const defaultSavedViewsTable = "nucleus_admin_saved_views"

// savedView is one stored query.
type savedView struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	IsShared  bool   `json:"is_shared"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

const savedViewNameMaxLen = 120
const savedViewQueryMaxLen = 4096

type savedViewStore struct {
	db     *sql.DB
	table  string
	system string
}

// savedViewsStore returns the store, or nil when the panel has no database
// handle to keep views in.
func (p *Panel) savedViewsStore() *savedViewStore {
	if p.db == nil {
		return nil
	}
	sqlDB, err := p.db.SqlDB()
	if err != nil || sqlDB == nil {
		return nil
	}
	return &savedViewStore{db: sqlDB, table: defaultSavedViewsTable, system: p.db.System()}
}

// ensureSavedViewsSchema creates the table if it is not there, in the dialect
// of the connection. It runs at mount, next to the audit trail's own schema.
func (s *savedViewStore) ensureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, savedViewsTableDDL(s.system, s.table))
	return err
}

func savedViewsTableDDL(system, table string) string {
	switch system {
	case "mssql":
		return fmt.Sprintf(`IF OBJECT_ID('%s', 'U') IS NULL
	CREATE TABLE %s (
		id NVARCHAR(64) NOT NULL PRIMARY KEY,
		owner NVARCHAR(256) NOT NULL,
		model_name NVARCHAR(256) NOT NULL,
		name NVARCHAR(191) NOT NULL,
		query NVARCHAR(MAX) NOT NULL,
		is_shared BIT NOT NULL DEFAULT 0,
		created_at NVARCHAR(64) NOT NULL,
		updated_at NVARCHAR(64) NOT NULL
	)`, table, table)
	case "oracle":
		return fmt.Sprintf(`BEGIN
	EXECUTE IMMEDIATE 'CREATE TABLE %s (
		id VARCHAR2(64) NOT NULL PRIMARY KEY,
		owner VARCHAR2(256) NOT NULL,
		model_name VARCHAR2(256) NOT NULL,
		name VARCHAR2(191) NOT NULL,
		query CLOB NOT NULL,
		is_shared NUMBER(1) DEFAULT 0 NOT NULL,
		created_at VARCHAR2(64) NOT NULL,
		updated_at VARCHAR2(64) NOT NULL
	)';
EXCEPTION
	WHEN OTHERS THEN
		IF SQLCODE != -955 THEN RAISE; END IF;
END;`, table)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id VARCHAR(64) PRIMARY KEY,
	owner VARCHAR(256) NOT NULL,
	model_name VARCHAR(256) NOT NULL,
	name VARCHAR(191) NOT NULL,
	query TEXT NOT NULL,
	is_shared INTEGER NOT NULL DEFAULT 0,
	created_at VARCHAR(64) NOT NULL,
	updated_at VARCHAR(64) NOT NULL
)`, table)
	}
}

const savedViewColumns = "id, owner, model_name, name, query, is_shared, created_at, updated_at"

func (s *savedViewStore) binds(n int) ([]string, error) {
	ph := bindPlaceholders(s.system, n)
	if ph == nil {
		return nil, fmt.Errorf("admin saved views: unknown database dialect %q", s.system)
	}
	return ph, nil
}

// list returns the views this operator can see for a model: their own, plus
// the shared ones.
func (s *savedViewStore) list(ctx context.Context, owner, modelName string) ([]savedView, error) {
	ph, err := s.binds(2)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE (owner = %s OR is_shared <> 0)", savedViewColumns, s.table, ph[0])
	args := []any{owner}
	if modelName != "" {
		query += " AND model_name = " + ph[1]
		args = append(args, modelName)
	}
	query += " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list saved views: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]savedView, 0, 8)
	for rows.Next() {
		view, err := scanSavedView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, rows.Err()
}

func scanSavedView(rows *sql.Rows) (savedView, error) {
	var (
		view                 savedView
		shared               any
		createdAt, updatedAt any
	)
	if err := rows.Scan(&view.ID, &view.Owner, &view.Model, &view.Name, &view.Query,
		&shared, &createdAt, &updatedAt); err != nil {
		return savedView{}, err
	}
	view.IsShared = parseAdminSuperuserValue(shared)
	view.CreatedAt = timestampText(createdAt)
	view.UpdatedAt = timestampText(updatedAt)
	return view, nil
}

func (s *savedViewStore) get(ctx context.Context, id string) (savedView, error) {
	ph, err := s.binds(1)
	if err != nil {
		return savedView{}, err
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", savedViewColumns, s.table, ph[0]), id)
	if err != nil {
		return savedView{}, fmt.Errorf("get saved view: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return savedView{}, errSavedViewNotFound
	}
	return scanSavedView(rows)
}

var errSavedViewNotFound = fmt.Errorf("saved view not found")

func (s *savedViewStore) create(ctx context.Context, view savedView) error {
	ph, err := s.binds(8)
	if err != nil {
		return err
	}
	shared := 0
	if view.IsShared {
		shared = 1
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		s.table, savedViewColumns, strings.Join(ph, ", ")),
		view.ID, view.Owner, view.Model, view.Name, view.Query, shared, view.CreatedAt, view.UpdatedAt)
	return err
}

func (s *savedViewStore) update(ctx context.Context, view savedView) error {
	ph, err := s.binds(5)
	if err != nil {
		return err
	}
	shared := 0
	if view.IsShared {
		shared = 1
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET name = %s, query = %s, is_shared = %s, updated_at = %s WHERE id = %s",
		s.table, ph[0], ph[1], ph[2], ph[3], ph[4]),
		view.Name, view.Query, shared, view.UpdatedAt, view.ID)
	return err
}

func (s *savedViewStore) delete(ctx context.Context, id string) error {
	ph, err := s.binds(1)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = %s", s.table, ph[0]), id)
	return err
}

func newSavedViewID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("view_%d", time.Now().UTC().UnixNano())
	}
	return "view_" + hex.EncodeToString(buf[:])
}

// --- HTTP ---------------------------------------------------------------

// savedViewOwner is the name a view belongs to. It is the operator's username
// for the same reason row ownership is (permissions_row.go): it is what
// policies and the audit trail already name them by.
func (p *Panel) savedViewOwner(r *http.Request) (string, error) {
	if p.config.Auth == nil {
		return "anonymous", nil
	}
	user, err := p.authenticatedUser(r)
	if err != nil {
		return "", p.authErrorToDomain(err)
	}
	if user == nil || user.Username == "" {
		return "", gferrors.Forbidden("this operator has no name to own a view by")
	}
	return user.Username, nil
}

func (p *Panel) savedViewsUnavailable() error {
	return &gferrors.DomainError{
		Code:       "NOT_IMPLEMENTED",
		Message:    "saved views need a database handle, and this panel has none",
		StatusCode: http.StatusNotImplemented,
	}
}

// The saved-view routes are gated by what a view POINTS AT, not by a
// permission of their own: creating one needs the list permission of its
// model, listing hides the ones whose model this operator cannot list, and
// editing needs ownership. A panel-wide "views" permission would have been a
// second thing to grant that answers no question the list permission does not.
func (p *Panel) handleListSavedViews(c *router.Context) error {
	store := p.savedViewsStore()
	if store == nil {
		return p.savedViewsUnavailable()
	}
	owner, err := p.savedViewOwner(c.Request)
	if err != nil {
		return err
	}
	views, err := store.list(c.Request.Context(), owner, strings.TrimSpace(c.Query("model")))
	if err != nil {
		return err
	}
	// A shared view of a model this operator cannot list is not theirs to
	// see: the row would tell them the model exists and what somebody filters
	// it by, which is exactly what the list permission withholds.
	visible := make([]savedView, 0, len(views))
	for _, view := range views {
		mi, ok := p.src.Get(view.Model)
		if !ok {
			continue
		}
		if _, err := p.authorizeRecordAction(c, mi, "list"); err != nil {
			continue
		}
		visible = append(visible, view)
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"views": visible})
}

type savedViewRequest struct {
	Model    string `json:"model"`
	Name     string `json:"name"`
	Query    string `json:"query"`
	IsShared bool   `json:"is_shared"`
}

func (req savedViewRequest) validate(p *Panel, requireModel bool) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return gferrors.BadRequest("name is required")
	}
	if len(name) > savedViewNameMaxLen {
		return gferrors.BadRequest(fmt.Sprintf("name is longer than %d characters", savedViewNameMaxLen))
	}
	if len(req.Query) > savedViewQueryMaxLen {
		return gferrors.BadRequest(fmt.Sprintf("query is longer than %d characters", savedViewQueryMaxLen))
	}
	if requireModel {
		model := strings.TrimSpace(req.Model)
		if model == "" {
			return gferrors.BadRequest("model is required")
		}
		// A view for a model that does not exist is a shortcut to a 404, and
		// it would sit in the list looking valid.
		if _, ok := p.src.Get(model); !ok {
			return gferrors.NotFound("model", model)
		}
	}
	return nil
}

func (p *Panel) handleCreateSavedView(c *router.Context) error {
	r := c.Request
	store := p.savedViewsStore()
	if store == nil {
		return p.savedViewsUnavailable()
	}
	owner, err := p.savedViewOwner(r)
	if err != nil {
		return err
	}

	var req savedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	if err := req.validate(p, true); err != nil {
		return err
	}
	// The view points at a model's list, so it is the list's permission that
	// decides whether this operator may have one at all.
	mi, _ := p.src.Get(strings.TrimSpace(req.Model))
	if _, err := p.authorizeRecordAction(c, mi, "list"); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	view := savedView{
		ID:        newSavedViewID(),
		Owner:     owner,
		Model:     mi.Name,
		Name:      strings.TrimSpace(req.Name),
		Query:     strings.TrimPrefix(strings.TrimSpace(req.Query), "?"),
		IsShared:  req.IsShared,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.create(r.Context(), view); err != nil {
		return fmt.Errorf("create saved view: %w", err)
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "view.create",
		ModelName: view.Model,
		RecordID:  view.ID,
		NewValue:  map[string]any{"name": view.Name, "query": view.Query, "is_shared": view.IsShared},
	})
	return c.JSON(http.StatusCreated, view)
}

// mayEditSavedView reports whether the request's operator owns the view, or
// is a superuser.
func (p *Panel) mayEditSavedView(r *http.Request, view savedView, owner string) bool {
	if view.Owner == owner {
		return true
	}
	if p.config.Auth == nil {
		return true
	}
	user, err := p.authenticatedUser(r)
	return err == nil && user != nil && user.IsSuperuser
}

func (p *Panel) handleUpdateSavedView(c *router.Context) error {
	r := c.Request
	store := p.savedViewsStore()
	if store == nil {
		return p.savedViewsUnavailable()
	}
	owner, err := p.savedViewOwner(r)
	if err != nil {
		return err
	}
	view, err := store.get(r.Context(), c.Param("id"))
	if err != nil {
		if err == errSavedViewNotFound {
			return gferrors.NotFound("saved view", c.Param("id"))
		}
		return err
	}
	// Somebody else's view is not theirs to change — and a shared one is not
	// found rather than forbidden only when it is not visible at all.
	if !p.mayEditSavedView(r, view, owner) {
		return gferrors.Forbidden("this view belongs to " + view.Owner)
	}

	var req savedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	if err := req.validate(p, false); err != nil {
		return err
	}

	before := map[string]any{"name": view.Name, "query": view.Query, "is_shared": view.IsShared}
	view.Name = strings.TrimSpace(req.Name)
	view.Query = strings.TrimPrefix(strings.TrimSpace(req.Query), "?")
	view.IsShared = req.IsShared
	view.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := store.update(r.Context(), view); err != nil {
		return fmt.Errorf("update saved view: %w", err)
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "view.update",
		ModelName: view.Model,
		RecordID:  view.ID,
		OldValue:  before,
		NewValue:  map[string]any{"name": view.Name, "query": view.Query, "is_shared": view.IsShared},
	})
	return c.JSON(http.StatusOK, view)
}

func (p *Panel) handleDeleteSavedView(c *router.Context) error {
	r := c.Request
	store := p.savedViewsStore()
	if store == nil {
		return p.savedViewsUnavailable()
	}
	owner, err := p.savedViewOwner(r)
	if err != nil {
		return err
	}
	view, err := store.get(r.Context(), c.Param("id"))
	if err != nil {
		if err == errSavedViewNotFound {
			return gferrors.NotFound("saved view", c.Param("id"))
		}
		return err
	}
	if !p.mayEditSavedView(r, view, owner) {
		return gferrors.Forbidden("this view belongs to " + view.Owner)
	}
	if err := store.delete(r.Context(), view.ID); err != nil {
		return fmt.Errorf("delete saved view: %w", err)
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "view.delete",
		ModelName: view.Model,
		RecordID:  view.ID,
		OldValue:  map[string]any{"name": view.Name, "query": view.Query, "is_shared": view.IsShared},
	})
	return c.JSON(http.StatusOK, map[string]interface{}{"deleted": true, "id": view.ID})
}
