package admin

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/internal/datasource"
)

// handleExportCSV exports all records of a model as CSV.
func (p *Panel) handleExportCSV(c *router.Context) error {
	w, r := c.Writer, c.Request
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "export_csv"); err != nil {
		return err
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}
	idSet, err := parseIDSet(c.Query("ids"))
	if err != nil {
		return gferrors.BadRequest("invalid ids query param")
	}
	page, err := st.List(r.Context(), datasource.Query{
		Page: 1, PageSize: 10000,
	})
	if err != nil {
		return err
	}

	// Determine visible columns and the primary-key field for id filtering.
	var headers []string
	var columns []datasource.FieldInfo
	var pkField datasource.FieldInfo
	var hasPK bool
	for _, f := range mi.Fields {
		if f.IsPK {
			pkField = f
			hasPK = true
		}
		if !f.IsExcluded {
			headers = append(headers, f.Label)
			columns = append(columns, f)
		}
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, mi.Table))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write(headers)

	for _, rec := range page.Items {
		if len(idSet) > 0 {
			id, ok := recordID(rec, pkField, hasPK)
			if !ok {
				continue
			}
			if _, exists := idSet[id]; !exists {
				continue
			}
		}
		row := make([]string, 0, len(columns))
		for _, f := range columns {
			if v, ok := recordValue(rec, f); ok {
				row = append(row, fmt.Sprintf("%v", v))
			} else {
				row = append(row, "")
			}
		}
		writer.Write(row)
	}
	return nil
}

func parseIDSet(raw string) (map[uint64]struct{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[uint64]struct{}{}, nil
	}

	set := make(map[uint64]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, err
		}
		set[id] = struct{}{}
	}
	return set, nil
}

// recordID reads a record's primary-key value and coerces it to uint64 for the
// id-set membership check. It prefers the model's PK field (when known) and
// falls back to the conventional "id" key. Values arrive from JSON records as
// float64, but int/uint/string forms are tolerated.
func recordID(rec datasource.Record, pkField datasource.FieldInfo, hasPK bool) (uint64, bool) {
	var v any
	var found bool
	if hasPK {
		v, found = recordValue(rec, pkField)
	}
	if !found {
		v, found = rec["id"]
	}
	if !found || v == nil {
		return 0, false
	}

	switch n := v.(type) {
	case float64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float32:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case uint:
		return uint64(n), true
	case uint64:
		return n, true
	case string:
		id, err := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		if err != nil {
			return 0, false
		}
		return id, true
	default:
		return 0, false
	}
}
