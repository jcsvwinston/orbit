package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/storage"
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
	Models   []string          `json:"models"`    // Models to export (empty = all registered)
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
	Filename   string    `json:"filename"`
	StorageKey string    `json:"storage_key"` // Key in storage for download
	Size       int64     `json:"size"`
	Records    int       `json:"records"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	URL        string    `json:"url,omitempty"` // Download URL when available
}

// ExportModels exports selected models to storage using the configured format.
func (p *Panel) exportModels(ctx context.Context, cfg ExportConfig) (ExportResult, error) {
	result := ExportResult{
		Status:    "processing",
		Format:    string(cfg.Format),
		CreatedAt: time.Now().UTC(),
	}

	if p.store == nil {
		return result, fmt.Errorf("storage not configured")
	}

	if len(cfg.Models) == 0 {
		for _, m := range p.registry.All() {
			cfg.Models = append(cfg.Models, m.Name)
		}
	}
	sort.Strings(cfg.Models)

	switch cfg.Format {
	case ExportFormatCSV:
		return p.exportCSV(ctx, cfg, result)
	case ExportFormatJSON:
		return p.exportJSON(ctx, cfg, result)
	case ExportFormatSQL:
		return p.exportSQL(ctx, cfg, result)
	default:
		return result, fmt.Errorf("unsupported export format: %s", cfg.Format)
	}
}

func (p *Panel) exportCSV(ctx context.Context, cfg ExportConfig, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := storage.CleanupTempKey("export") + fmt.Sprintf("_%s.csv", ts)

	buf := &bytes.Buffer{}
	writer := csv.NewWriter(buf)
	totalRecords := 0
	headerWritten := false

	for _, modelName := range cfg.Models {
		meta, ok := p.registry.Get(modelName)
		if !ok {
			continue
		}

		crud, err := p.getCRUD(meta, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export CSV model %s: %w", modelName, err)
		}

		parsed, err := crud.FindAll(ctx, model.QueryOpts{
			Page: 1, PageSize: 10000,
			Filters: p.applyTenantFilter(cfg.TenantID, cfg.Filters, meta),
		})
		if err != nil {
			return result, fmt.Errorf("export CSV fetch %s: %w", modelName, err)
		}

		items := reflect.ValueOf(parsed.Items)
		if items.Kind() == reflect.Ptr {
			items = items.Elem()
		}

		if !headerWritten {
			headers := []string{"_model"}
			for _, f := range meta.Fields {
				if !f.IsExcluded {
					headers = append(headers, f.Column)
				}
			}
			writer.Write(headers)
			headerWritten = true
		}

		for i := 0; i < items.Len(); i++ {
			row := []string{modelName}
			item := items.Index(i)
			for _, f := range meta.Fields {
				if f.IsExcluded {
					continue
				}
				val := item.FieldByName(f.Name)
				if !val.IsValid() {
					row = append(row, "")
					continue
				}
				row = append(row, formatFieldValue(val.Interface()))
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

func (p *Panel) exportJSON(ctx context.Context, cfg ExportConfig, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := storage.CleanupTempKey("export") + fmt.Sprintf("_%s.json", ts)

	allRecords := []map[string]interface{}{}
	totalRecords := 0

	for _, modelName := range cfg.Models {
		meta, ok := p.registry.Get(modelName)
		if !ok {
			continue
		}

		crud, err := p.getCRUD(meta, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export JSON model %s: %w", modelName, err)
		}

		parsed, err := crud.FindAll(ctx, model.QueryOpts{
			Page: 1, PageSize: 10000,
			Filters: p.applyTenantFilter(cfg.TenantID, cfg.Filters, meta),
		})
		if err != nil {
			return result, fmt.Errorf("export JSON fetch %s: %w", modelName, err)
		}

		items := reflect.ValueOf(parsed.Items)
		if items.Kind() == reflect.Ptr {
			items = items.Elem()
		}

		for i := 0; i < items.Len(); i++ {
			item := items.Index(i)
			data := entityToMap(meta, item.Addr().Interface())
			data["_model"] = modelName
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

func (p *Panel) exportSQL(ctx context.Context, cfg ExportConfig, result ExportResult) (ExportResult, error) {
	ts := time.Now().UTC().Format("20060102150405")
	key := storage.CleanupTempKey("export") + fmt.Sprintf("_%s.sql", ts)

	buf := &bytes.Buffer{}
	totalRecords := 0

	buf.WriteString("-- Nucleus SQL Dump\n")
	buf.WriteString(fmt.Sprintf("-- Generated: %s\n", ts))
	buf.WriteString(fmt.Sprintf("-- Database: %s\n\n", cfg.Database))

	for _, modelName := range cfg.Models {
		meta, ok := p.registry.Get(modelName)
		if !ok {
			continue
		}

		// Schema
		buf.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", meta.Table))
		cols := []string{}
		for _, f := range meta.Fields {
			if f.IsExcluded {
				continue
			}
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
		crud, err := p.getCRUD(meta, cfg.Database)
		if err != nil {
			return result, fmt.Errorf("export SQL model %s: %w", modelName, err)
		}

		parsed, err := crud.FindAll(ctx, model.QueryOpts{
			Page: 1, PageSize: 10000,
			Filters: p.applyTenantFilter(cfg.TenantID, cfg.Filters, meta),
		})
		if err != nil {
			return result, fmt.Errorf("export SQL fetch %s: %w", modelName, err)
		}

		items := reflect.ValueOf(parsed.Items)
		if items.Kind() == reflect.Ptr {
			items = items.Elem()
		}

		columnNames := []string{}
		for _, f := range meta.Fields {
			if !f.IsExcluded {
				columnNames = append(columnNames, f.Column)
			}
		}

		for i := 0; i < items.Len(); i++ {
			item := items.Index(i)
			values := []string{}
			for _, f := range meta.Fields {
				if f.IsExcluded {
					continue
				}
				val := item.FieldByName(f.Name)
				values = append(values, sqlValue(val.Interface()))
			}

			buf.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
				meta.Table,
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

func (p *Panel) applyTenantFilter(tenantID string, filters map[string]string, meta *model.ModelMeta) map[string]string {
	if tenantID == "" {
		return filters
	}
	tenantField := meta.TenantFieldName()
	if tenantField == "" {
		return filters
	}
	if filters == nil {
		filters = make(map[string]string)
	}
	filters[tenantField] = tenantID
	return filters
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

// listExportJobs returns all completed exports for download.
func (p *Panel) listExportJobs() []ExportResult {
	if p.exportResults == nil {
		return []ExportResult{}
	}
	p.exportMu.RLock()
	defer p.exportMu.RUnlock()

	results := make([]ExportResult, 0, len(p.exportResults))
	for _, r := range p.exportResults {
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
