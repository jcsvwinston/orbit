package nucleus

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/model"

	"github.com/jcsvwinston/orbit/datasource"
)

// store implements datasource.RecordStore for one (model, alias) pair. It owns
// the reflection and dialect-specific counting the panel used to carry.
type store struct {
	meta    *model.ModelMeta
	crud    model.CRUDOperator
	sqlDB   *sql.DB
	dialect string // lower-cased
}

var _ datasource.RecordStore = (*store)(nil)

// List runs a paginated query and maps the native envelope to datasource.Page.
// Item order and JSON shape are preserved via entityToRecord (ADR-001 O3).
func (s *store) List(ctx context.Context, q datasource.Query) (datasource.Page, error) {
	res, err := s.crud.FindAll(ctx, model.QueryOpts{
		Page:     q.Page,
		PageSize: q.PageSize,
		Search:   q.Search,
		Filters:  q.Filters,
		OrderBy:  q.OrderBy,
	})
	if err != nil {
		return datasource.Page{}, err
	}

	items, err := entitiesToRecords(res.Items)
	if err != nil {
		return datasource.Page{}, err
	}
	return datasource.Page{
		Items:       items,
		Total:       res.Total,
		Page:        res.Page,
		PageSize:    res.PageSize,
		TotalPages:  res.TotalPages,
		IsEstimated: res.IsEstimated,
		HasMore:     res.HasMore,
	}, nil
}

// entitiesToRecords converts the interface{}-wrapped slice FindAll returns into
// a []Record, tolerating both value and pointer element kinds.
func entitiesToRecords(items any) ([]datasource.Record, error) {
	if items == nil {
		return []datasource.Record{}, nil
	}
	v := reflect.ValueOf(items)
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("datasource/nucleus: FindAll items is %s, want slice", v.Kind())
	}
	out := make([]datasource.Record, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		rec, err := entityToRecord(v.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Get fetches one record by string id (narrowed to uint for Nucleus — D1).
func (s *store) Get(ctx context.Context, id string) (datasource.Record, error) {
	pk, err := s.parseID(id)
	if err != nil {
		return nil, err
	}
	entity, err := s.crud.FindByID(ctx, pk)
	if err != nil {
		return nil, err
	}
	return entityToRecord(entity)
}

// Create inserts a record and returns the created row.
func (s *store) Create(ctx context.Context, rec datasource.Record) (datasource.Record, error) {
	entity, err := payloadToEntity(s.meta, rec)
	if err != nil {
		return nil, err
	}
	if err := s.crud.Create(ctx, entity); err != nil {
		return nil, err
	}
	return entityToRecord(entity)
}

// Update applies a partial change set. The record is forwarded as a column→value
// map, matching the framework CRUD's update contract.
func (s *store) Update(ctx context.Context, id string, rec datasource.Record) error {
	pk, err := s.parseID(id)
	if err != nil {
		return err
	}
	return s.crud.Update(ctx, pk, map[string]any(rec))
}

// Delete removes one record by id.
func (s *store) Delete(ctx context.Context, id string) error {
	pk, err := s.parseID(id)
	if err != nil {
		return err
	}
	return s.crud.Delete(ctx, pk)
}

// parseID narrows a boundary string id to the uint Nucleus PKs use (D1). A
// backend with uuid/string/composite keys would narrow differently.
func (s *store) parseID(id string) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("datasource/nucleus: invalid id %q", id)
	}
	return uint(n), nil
}

// Count returns a (possibly estimated) row count using the dialect's statistics
// table, falling back to COUNT(*). Present is false when the table is absent.
// This is the panel's old modelCount, relocated behind the contract.
func (s *store) Count(ctx context.Context) (datasource.CountResult, error) {
	table := s.table()

	var query string
	estimated := false
	switch s.dialect {
	case "postgres":
		query = "SELECT reltuples::bigint FROM pg_class WHERE relname = ?"
		estimated = true
	case "mysql":
		query = "SELECT TABLE_ROWS FROM information_schema.tables WHERE table_name = ? AND table_schema = DATABASE()"
		estimated = true
	case "sqlite", "sqlite3":
		query = "SELECT n FROM sqlite_stat1 WHERE tbl = ? LIMIT 1"
		estimated = true
	case "sqlserver", "mssql":
		query = "SELECT SUM(rows) FROM sys.partitions WHERE object_id = OBJECT_ID(?) AND index_id IN (0, 1)"
		estimated = true
	case "oracle":
		query = "SELECT NUM_ROWS FROM ALL_TABLES WHERE TABLE_NAME = UPPER(?)"
		estimated = true
	default:
		query = fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	}

	var total int64
	var qErr error
	if estimated {
		qErr = s.sqlDB.QueryRowContext(ctx, query, table).Scan(&total)
		if qErr != nil {
			// Estimate failed or table not analyzed: fall back to a real count.
			qErr = s.sqlDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&total)
			estimated = false
		}
	} else {
		qErr = s.sqlDB.QueryRowContext(ctx, query).Scan(&total)
	}

	if qErr != nil {
		if isTableMissingErr(qErr) {
			return datasource.CountResult{Present: false}, nil
		}
		return datasource.CountResult{}, fmt.Errorf("datasource/nucleus: count table=%s: %w", table, qErr)
	}
	return datasource.CountResult{Count: total, IsEstimated: estimated, Present: true}, nil
}

// TableExists reports whether the backing table is queryable.
func (s *store) TableExists(ctx context.Context) bool {
	rows, err := s.sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s WHERE 1=0", s.table()))
	if err != nil {
		return false
	}
	_ = rows.Close()
	return rows.Err() == nil
}

func (s *store) table() string {
	if t := strings.TrimSpace(s.meta.Table); t != "" {
		return t
	}
	return strings.ToLower(s.meta.Name) + "s"
}

func isTableMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "undefined table")
}
