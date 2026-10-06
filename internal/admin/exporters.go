package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"

	"github.com/jcsvwinston/orbit/datasource"
)

// ExportFormat defines supported export formats.
type ExportFormat string

const (
	ExportFormatCSV  ExportFormat = "csv"
	ExportFormatJSON ExportFormat = "json"
	ExportFormatSQL  ExportFormat = "sql"
)

// ExportConfig defines the scope and format of an export operation.
type ExportConfig struct {
	Models   []string          `json:"models"`    // Models to export (empty = every one the operator may list)
	Database string            `json:"database"`  // Source database alias
	TenantID string            `json:"tenant_id"` // Tenant scope (empty = all)
	Format   ExportFormat      `json:"format"`    // csv | json | sql
	Filters  map[string]string `json:"filters"`   // Additional filters (model.field=value)
}

// ExportResult holds the result of an export operation.
type ExportResult struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"` // completed, processing, failed
	Format     string    `json:"format"`
	Tenant     string    `json:"tenant,omitempty"` // tenant the export was confined to ("" = every tenant)
	Filename   string    `json:"filename"`
	StorageKey string    `json:"storage_key"` // Key in storage for download
	Size       int64     `json:"size"`
	Records    int       `json:"records"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	URL        string    `json:"url,omitempty"` // Download URL when available

	// producer names the operator the export was cut for (exportProducer).
	// An export is a copy of what that operator could read (OR-66), so it is
	// listed, polled and downloaded by them and by a superuser only. "" on a
	// panel with no operators (no auth provider).
	producer string
}

// exportTarget is one model of an export or a fixture dump, what the
// requesting operator may read of it, and the filter its rows are read by.
type exportTarget struct {
	model   datasource.ModelInfo
	read    readScope
	filters map[string]string
}

// exportTargets resolves the models an export or a dump walks and, for each,
// what the requesting operator may read of it: the rows and the fields `list`
// would show them (OR-66). export_data and the dump are granted on admin:*,
// and before this they walked every model with no confinement but the
// tenant's — every row and every field, of the models the operator could not
// even open.
//
// A model the request names that the operator may not list refuses the whole
// export, with the answer list gives (a 403, including the one an #own grant
// the panel cannot honour gets). An export of every model — none named —
// leaves out the ones the operator may not list, the way the sidebar does. A
// name the registry does not know is skipped, as it always was.
//
// body is the request's own filters and tenant the tenant an unscoped request
// narrowed the export to (exportFilters).
func (p *Panel) exportTargets(c *router.Context, requested []string, body map[string]string, tenant string) ([]exportTarget, error) {
	explicit := len(requested) > 0
	var infos []datasource.ModelInfo
	if explicit {
		for _, name := range requested {
			if mi, ok := p.src.Get(name); ok {
				infos = append(infos, mi)
			}
		}
	} else {
		infos = p.src.All()
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	targets := make([]exportTarget, 0, len(infos))
	seen := make(map[string]bool, len(infos))
	for _, mi := range infos {
		if seen[mi.Name] {
			continue
		}
		seen[mi.Name] = true
		read, err := p.requestReadScope(c, mi, "list")
		if err != nil {
			if explicit {
				return nil, err
			}
			continue
		}
		filters, err := p.exportFilters(mi, read, body, tenant)
		if err != nil {
			return nil, err
		}
		targets = append(targets, exportTarget{model: mi, read: read, filters: filters})
	}
	return targets, nil
}

// exportFilters is what an export reads mi's rows by: the request body's own
// filters, the tenant an unscoped request narrowed the export to, and the
// operator's read scope laid over both. body is not modified: it is shared by
// every model of the export.
//
// A body filter is keyed by the runtime column it resolves to, as the list
// keys its own, so the scope replaces one on a confined column instead of
// sitting beside it for a backend to choose between; one that names no field
// of mi is another model's, and is not applied to this one (the backends
// dropped it anyway). A filter on a field this operator does not read
// (fieldRules.readsField, the predicate the list's filters are held to as
// well) is refused with the list's answer: the number of rows it leaves would
// say what the field holds, which is the value the export leaves out.
func (p *Panel) exportFilters(mi datasource.ModelInfo, read readScope, body map[string]string, tenant string) (map[string]string, error) {
	base := make(map[string]string, len(body)+1)
	for key, value := range body {
		col, field, ok := dsResolveField(mi, key)
		if !ok {
			continue
		}
		if !read.fields.readsField(field) {
			return nil, gferrors.BadRequest(fmt.Sprintf("invalid filter field %q", key))
		}
		base[col] = value
	}
	if scope := p.importTenantScope(mi, tenant); scope.Enforced() {
		base[scope.Column()] = scope.Tenant
	}
	return read.filters(base), nil
}

// exportKey is where an export or a dump is written: under the exporter's
// namespace (isExportStorageKey for exports), stamped with the time and a
// random suffix. Two exports cut in the same second used to share a key, so
// the second overwrote the first and the first's producer was handed the
// second's rows; and a key made of the time alone could be guessed.
func exportKey(purpose, ext string) string {
	var buf [8]byte
	suffix := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	if _, err := rand.Read(buf[:]); err == nil {
		suffix = hex.EncodeToString(buf[:])
	}
	return storage.CleanupTempKey(purpose) + "_" + suffix + "." + ext
}

// exportProducer is the name an export is recorded under for user: their id,
// or their username when they have none. "" for no operator.
func exportProducer(user *auth.User) string {
	switch {
	case user == nil:
		return ""
	case user.ID != "":
		return "id:" + user.ID
	case user.Username != "":
		return "username:" + user.Username
	}
	return ""
}

// exportProducerOf is the producer an export cut by r is recorded under.
func (p *Panel) exportProducerOf(r *http.Request) string {
	if p.config.Auth == nil {
		return ""
	}
	user, err := p.authenticatedUser(r)
	if err != nil {
		return ""
	}
	return exportProducer(user)
}

// exportViewer is who asks for a recorded export — the job list, a status, a
// download: the tenant their request is confined to ("" when it is not), and
// either every operator's exports (a superuser, or a panel with no operators)
// or the ones cut for producer.
type exportViewer struct {
	tenant   string
	all      bool
	producer string
}

// exportViewerOf resolves the viewer r is. An operator the panel cannot
// resolve sees no export at all.
func (p *Panel) exportViewerOf(r *http.Request) exportViewer {
	v := exportViewer{tenant: p.enforcedTenantID(r)}
	if p.config.Auth == nil {
		v.all = true
		return v
	}
	user, err := p.authenticatedUser(r)
	if err != nil || user == nil {
		return v
	}
	if user.IsSuperuser {
		v.all = true
		return v
	}
	v.producer = exportProducer(user)
	return v
}

// confined reports whether the viewer may see only some exports, so a
// download has to find its key in the job registry first.
func (v exportViewer) confined() bool { return v.tenant != "" || !v.all }

// sees reports whether job may be listed, polled or downloaded by v: one cut
// for v's tenant when v is confined to one, and cut for v unless v sees
// every operator's.
func (v exportViewer) sees(job ExportResult) bool {
	if v.tenant != "" && job.Tenant != v.tenant {
		return false
	}
	return v.all || (v.producer != "" && job.producer == v.producer)
}

// ExportModels exports the targets to storage using the configured format.
// Each model's rows and columns are the ones its target's read scope lets
// the requesting operator see; a model with no target is not exported.
func (p *Panel) exportModels(ctx context.Context, cfg ExportConfig, targets []exportTarget) (ExportResult, error) {
	result := ExportResult{
		Status:    "processing",
		Format:    string(cfg.Format),
		CreatedAt: time.Now().UTC(),
	}

	if p.store == nil {
		return result, fmt.Errorf("storage not configured")
	}

	switch cfg.Format {
	case ExportFormatCSV:
		return p.exportCSV(ctx, cfg, targets, result)
	case ExportFormatJSON:
		return p.exportJSON(ctx, cfg, targets, result)
	case ExportFormatSQL:
		return p.exportSQL(ctx, cfg, targets, result)
	default:
		return result, fmt.Errorf("unsupported export format: %s", cfg.Format)
	}
}

func (p *Panel) exportCSV(ctx context.Context, cfg ExportConfig, targets []exportTarget, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := exportKey("export", "csv")

	buf := &bytes.Buffer{}
	writer := csv.NewWriter(buf)
	totalRecords := 0
	headerWritten := false

	for _, t := range targets {
		mi := t.model
		st, err := p.src.Store(mi.Name, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export CSV model %s: %w", mi.Name, err)
		}

		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 10000,
			Filters: t.filters,
		})
		if err != nil {
			return result, fmt.Errorf("export CSV fetch %s: %w", mi.Name, err)
		}

		// A field this operator may not read is neither a column nor a
		// value of their export.
		columns := t.read.readableFields(mi)
		if !headerWritten {
			headers := []string{"_model"}
			for _, f := range columns {
				headers = append(headers, f.Column)
			}
			writer.Write(headers)
			headerWritten = true
		}

		for _, rec := range page.Items {
			row := []string{mi.Name}
			for _, f := range columns {
				v, ok := recordValue(rec, f)
				if !ok {
					row = append(row, "")
					continue
				}
				row = append(row, formatFieldValue(v))
			}
			writer.Write(row)
			totalRecords++
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return result, fmt.Errorf("export CSV write: %w", err)
	}

	info, err := p.store.Put(ctx, key, bytes.NewReader(buf.Bytes()), storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: "text/csv",
	})
	if err != nil {
		return result, fmt.Errorf("export CSV store: %w", err)
	}

	return finalizeExport(result, info, totalRecords, fmt.Sprintf("export_%s.csv", ts), p.store, ctx)
}

func (p *Panel) exportJSON(ctx context.Context, cfg ExportConfig, targets []exportTarget, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := exportKey("export", "json")

	allRecords := []map[string]interface{}{}
	totalRecords := 0

	for _, t := range targets {
		mi := t.model
		st, err := p.src.Store(mi.Name, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export JSON model %s: %w", mi.Name, err)
		}

		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 10000,
			Filters: t.filters,
		})
		if err != nil {
			return result, fmt.Errorf("export JSON fetch %s: %w", mi.Name, err)
		}

		// The record as the list hands it to this operator: masked by
		// the same field rules.
		t.read.fields.maskAll(mi, page.Items)
		for _, rec := range page.Items {
			data := map[string]interface{}(rec)
			data["_model"] = mi.Name
			allRecords = append(allRecords, data)
			totalRecords++
		}
	}

	jsonData, err := json.MarshalIndent(allRecords, "", "  ")
	if err != nil {
		return result, fmt.Errorf("export JSON marshal: %w", err)
	}

	info, err := p.store.Put(ctx, key, bytes.NewReader(jsonData), storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: "application/json",
	})
	if err != nil {
		return result, fmt.Errorf("export JSON store: %w", err)
	}

	return finalizeExport(result, info, totalRecords, fmt.Sprintf("export_%s.json", ts), p.store, ctx)
}

func (p *Panel) exportSQL(ctx context.Context, cfg ExportConfig, targets []exportTarget, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := exportKey("export", "sql")

	buf := &bytes.Buffer{}
	totalRecords := 0

	buf.WriteString("-- Nucleus SQL Dump\n")
	buf.WriteString(fmt.Sprintf("-- Generated: %s\n", ts))
	buf.WriteString(fmt.Sprintf("-- Database: %s\n\n", cfg.Database))

	for _, t := range targets {
		mi := t.model
		modelName := mi.Name
		// A field this operator may not read is in neither the table this
		// dump creates nor the rows it inserts.
		columns := t.read.readableFields(mi)

		// Schema
		buf.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", mi.Table))
		cols := []string{}
		for _, f := range columns {
			sqlType := goTypeToSQL(f.GoType, f.IsPK)
			constraints := []string{}
			if f.IsPK {
				constraints = append(constraints, "PRIMARY KEY")
			}
			if f.IsRequired {
				constraints = append(constraints, "NOT NULL")
			}
			colDef := fmt.Sprintf("  %s %s", f.Column, sqlType)
			if len(constraints) > 0 {
				colDef += " " + strings.Join(constraints, " ")
			}
			cols = append(cols, colDef)
		}
		buf.WriteString(strings.Join(cols, ",\n"))
		buf.WriteString("\n);\n\n")

		// Data
		st, err := p.src.Store(mi.Name, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export SQL model %s: %w", modelName, err)
		}

		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 10000,
			Filters: t.filters,
		})
		if err != nil {
			return result, fmt.Errorf("export SQL fetch %s: %w", modelName, err)
		}

		columnNames := make([]string, 0, len(columns))
		for _, f := range columns {
			columnNames = append(columnNames, f.Column)
		}

		for _, rec := range page.Items {
			values := []string{}
			for _, f := range columns {
				v, _ := recordValue(rec, f)
				values = append(values, sqlValue(v))
			}

			buf.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
				mi.Table,
				strings.Join(columnNames, ", "),
				strings.Join(values, ", "),
			))
			totalRecords++
		}
		buf.WriteString("\n")
	}

	info, err := p.store.Put(ctx, key, bytes.NewReader(buf.Bytes()), storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: "application/sql",
	})
	if err != nil {
		return result, fmt.Errorf("export SQL store: %w", err)
	}

	return finalizeExport(result, info, totalRecords, fmt.Sprintf("export_%s.sql", ts), p.store, ctx)
}

func finalizeExport(result ExportResult, info storage.ObjectInfo, records int, filename string, store storage.Store, ctx context.Context) (ExportResult, error) {
	result.Status = "completed"
	result.Filename = filename
	result.StorageKey = info.Key
	result.Size = info.Size
	result.Records = records

	url, err := store.SignedURL(ctx, info.Key, 24*time.Hour, storage.URLConfig{
		Disposition: "attachment",
	})
	if err == nil {
		result.URL = url
	}
	return result, nil
}

func formatFieldValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func goTypeToSQL(goType string, isPK bool) string {
	switch goType {
	case "string":
		return "TEXT"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "INTEGER"
	case "float32", "float64":
		return "REAL"
	case "bool":
		return "BOOLEAN"
	case "time.Time", "Time":
		return "TIMESTAMP"
	default:
		return "TEXT"
	}
}

func sqlValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(val, "'", "''") + "'"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case time.Time:
		return "'" + val.Format("2006-01-02 15:04:05") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", val), "'", "''") + "'"
	}
}

// listExportJobs returns the recorded exports viewer may download. An export
// is data of the tenant it was cut for, so a scoped request does not see one
// cut for another tenant or for all of them; and it is a copy of what its
// producer could read, so an operator does not see one cut for somebody
// else (a superuser sees every one).
func (p *Panel) listExportJobs(viewer exportViewer) []ExportResult {
	if p.exportResults == nil {
		return []ExportResult{}
	}
	p.exportMu.RLock()
	defer p.exportMu.RUnlock()

	results := make([]ExportResult, 0, len(p.exportResults))
	for _, r := range p.exportResults {
		if !viewer.sees(r) {
			continue
		}
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results
}

func (p *Panel) getExportJob(id string) (ExportResult, bool) {
	if p.exportResults == nil {
		return ExportResult{}, false
	}
	p.exportMu.RLock()
	defer p.exportMu.RUnlock()
	r, ok := p.exportResults[id]
	return r, ok
}
