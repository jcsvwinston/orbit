package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/orbit/datasource"
)

// ImportConfig defines the target and behavior of an import.
type ImportConfig struct {
	Database   string `json:"database"`    // Target database alias
	TenantID   string `json:"tenant_id"`   // Target tenant (for tenant field injection)
	Model      string `json:"model"`       // Target model
	Format     string `json:"format"`      // csv | json
	OnConflict string `json:"on_conflict"` // skip | update | error
	DryRun     bool   `json:"dry_run"`     // Validate only, no changes
	BatchSize  int    `json:"batch_size"`  // Records per batch (default 100)
}

// ImportError describes a single row import error.
type ImportError struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ImportReport summarizes import results.
type ImportReport struct {
	Total    int           `json:"total"`
	Imported int           `json:"imported"`
	Skipped  int           `json:"skipped"`
	Updated  int           `json:"updated"`
	Failed   int           `json:"failed"`
	Errors   []ImportError `json:"errors"`
	DryRun   bool          `json:"dry_run"`
}

// ValidateImportConfig checks import configuration.
func ValidateImportConfig(cfg ImportConfig, src datasource.DataSource) error {
	if cfg.Model == "" {
		return fmt.Errorf("model is required")
	}
	if _, ok := src.Get(cfg.Model); !ok {
		return fmt.Errorf("model %q not found", cfg.Model)
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	switch cfg.OnConflict {
	case "", "skip", "update", "error":
		// valid
	default:
		return fmt.Errorf("invalid on_conflict value: %q", cfg.OnConflict)
	}
	return nil
}

// ParseImportData parses uploaded data and returns a slice of record maps.
func ParseImportData(reader io.Reader, format string) ([]map[string]interface{}, error) {
	switch strings.ToLower(format) {
	case "csv":
		return parseCSVData(reader)
	case "json":
		return parseJSONData(reader)
	default:
		return nil, fmt.Errorf("unsupported import format: %s", format)
	}
}

func parseCSVData(reader io.Reader) ([]map[string]interface{}, error) {
	csvReader := csv.NewReader(reader)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV must have header row and at least one data row")
	}

	headers := records[0]
	results := make([]map[string]interface{}, 0, len(records)-1)

	for _, row := range records[1:] {
		record := make(map[string]interface{})
		for i, header := range headers {
			header = strings.TrimSpace(header)
			if header == "" {
				continue
			}
			if i < len(row) {
				record[header] = strings.TrimSpace(row[i])
			} else {
				record[header] = ""
			}
		}
		results = append(results, record)
	}
	return results, nil
}

func parseJSONData(reader io.Reader) ([]map[string]interface{}, error) {
	var records []map[string]interface{}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read JSON: %w", err)
	}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return records, nil
}

// ValidateImportData validates records against model schema without importing.
func ValidateImportData(mi datasource.ModelInfo, records []map[string]interface{}, tenantID string) []ImportError {
	errors := make([]ImportError, 0)

	for rowIdx, record := range records {
		// Check _model field for multi-model exports
		if modelField, ok := record["_model"]; ok && modelField != nil {
			if mf, ok := modelField.(string); ok && mf != "" && mf != mi.Name {
				// This record is for a different model
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Message: fmt.Sprintf("record belongs to model %q, expected %q", mf, mi.Name),
				})
				continue
			}
		}

		// Validate fields
		for col, val := range record {
			if col == "_model" {
				continue
			}

			field := dsFindFieldByColumn(mi, col)
			if field == nil {
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Field:   col,
					Message: fmt.Sprintf("unknown column %q", col),
				})
				continue
			}

			if field.IsReadOnly || field.IsExcluded {
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Field:   col,
					Message: fmt.Sprintf("field %q is read-only or excluded", col),
				})
				continue
			}

			if val == nil || val == "" {
				if field.IsRequired && !field.IsPK {
					errors = append(errors, ImportError{
						Row:     rowIdx,
						Field:   col,
						Message: fmt.Sprintf("field %q is required", col),
					})
				}
				continue
			}

			// Type validation
			strVal := fmt.Sprintf("%v", val)
			if err := validateFieldValue(*field, strVal); err != nil {
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Field:   col,
					Message: err.Error(),
				})
			}
		}

		// Tenant field check
		tenantField := mi.TenantField
		if tenantField != "" && tenantID != "" {
			// Tenant will be auto-injected, no validation needed
		}
	}

	return errors
}

func validateFieldValue(field datasource.FieldInfo, value string) error {
	if value == "" {
		return nil
	}
	switch field.GoType {
	case "int", "int8", "int16", "int32", "int64":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("invalid integer value: %q", value)
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("invalid unsigned integer value: %q", value)
		}
	case "float32", "float64":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("invalid float value: %q", value)
		}
	case "bool":
		lower := strings.ToLower(value)
		if lower != "true" && lower != "false" && lower != "1" && lower != "0" {
			return fmt.Errorf("invalid boolean value: %q", value)
		}
	case "time.Time", "Time":
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			if _, err2 := time.Parse("2006-01-02 15:04:05", value); err2 != nil {
				if _, err3 := time.Parse("2006-01-02", value); err3 != nil {
					return fmt.Errorf("invalid datetime value: %q", value)
				}
			}
		}
	}
	return nil
}

// ExecuteImport imports validated records into the database.
func (p *Panel) ExecuteImport(ctx context.Context, cfg ImportConfig, records []map[string]interface{}) (*ImportReport, error) {
	mi, ok := p.src.Get(cfg.Model)
	if !ok {
		return nil, fmt.Errorf("model %q not found", cfg.Model)
	}

	st, err := p.src.Store(mi.Name, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("get store for model %s: %w", cfg.Model, err)
	}

	report := &ImportReport{
		Total:  len(records),
		DryRun: cfg.DryRun,
		Errors: make([]ImportError, 0),
	}

	if cfg.DryRun {
		// Validation already done, just report
		return report, nil
	}

	scope := importTenantScope(mi, cfg.TenantID)
	for rowIdx, record := range records {
		// An import that targets a tenant writes every row into it: a row
		// naming another tenant (under any spelling of the column) fails
		// instead of landing there, a row naming none gets the tenant
		// stamped.
		if scope.Enforced() {
			if err := scope.guardPayload(record, true); err != nil {
				report.Failed++
				report.Errors = append(report.Errors, ImportError{
					Row:     rowIdx,
					Field:   mi.TenantField,
					Message: err.Error(),
				})
				continue
			}
		}

		// Check for existing record (for on_conflict handling)
		if cfg.OnConflict == "skip" || cfg.OnConflict == "update" {
			existingID, existing, err := p.findExistingByUniqueFields(ctx, st, mi, record, scope)
			if err == nil && existingID != "" {
				// The row the import matched must be the tenant's: a row
				// found by a pk of another tenant is neither skipped (that
				// would confirm it) nor updated (that overwrote it with the
				// stamped tenant, moving it). Not found, as a get answers.
				if scope.Enforced() && !scope.contains(existing) {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Row:     rowIdx,
						Message: fmt.Sprintf("record %s not found in tenant %q", existingID, cfg.TenantID),
					})
					continue
				}
				if cfg.OnConflict == "skip" {
					report.Skipped++
					continue
				}
				if cfg.OnConflict == "update" {
					// Update existing
					updates := make(map[string]interface{})
					for col, val := range record {
						if col == "_model" {
							continue
						}
						field := dsFindFieldByColumn(mi, col)
						if field != nil && !field.IsPK && !field.IsReadOnly {
							updates[col] = val
						}
					}
					if err := st.Update(ctx, existingID, datasource.Record(updates)); err != nil {
						report.Failed++
						report.Errors = append(report.Errors, ImportError{
							Row:     rowIdx,
							Message: fmt.Sprintf("update failed: %v", err),
						})
					} else {
						report.Updated++
					}
					continue
				}
			}
		}

		// Create new record
		if _, err := st.Create(ctx, datasource.Record(record)); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, ImportError{
				Row:     rowIdx,
				Message: fmt.Sprintf("create failed: %v", err),
			})
		} else {
			report.Imported++
		}
	}

	return report, nil
}

// findExistingByUniqueFields looks up an existing record by primary key or by a
// fully-populated unique index, returning its string ID (D1) and the row, or ""
// when none is found. It operates over the neutral datasource contract. The
// unique-index lookup stays inside the scope's tenant, so a value that is
// unique across tenants never matches another tenant's row; the pk lookup is
// unscoped and the caller decides what a row of another tenant means.
func (p *Panel) findExistingByUniqueFields(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, record map[string]interface{}, scope tenantScope) (string, datasource.Record, error) {
	// Try to find by primary key first
	if pkVal, ok := recordPrimaryKey(record, mi); ok {
		idStr := fmt.Sprintf("%v", pkVal)
		if existing, err := st.Get(ctx, idStr); err == nil {
			return idStr, existing, nil
		}
	}

	// Try to find by unique indexes
	for _, idx := range mi.Indexes {
		if !idx.Unique {
			continue
		}
		// Build filter for unique index columns
		filters := make(map[string]string)
		allPresent := true
		for _, col := range idx.Columns {
			if val, ok := record[col]; ok && val != nil && val != "" {
				filters[col] = fmt.Sprintf("%v", val)
			} else {
				allPresent = false
				break
			}
		}
		if !allPresent {
			continue
		}
		if scope.Enforced() {
			filters[scope.Column()] = scope.Tenant
		}

		// Query with unique index filter
		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 1, Filters: filters,
		})
		if err == nil && len(page.Items) > 0 {
			found := page.Items[0]
			if v := recordPKValue(found, mi); v != nil {
				return fmt.Sprintf("%v", v), found, nil
			}
		}
	}

	return "", nil, fmt.Errorf("not found")
}

// recordPrimaryKey reads the primary key an import row names: under
// ModelInfo.PrimaryKey, or the pk field's column or Go name in any letter
// case — the way the backend resolves payload keys. A CSV "id" header used
// to miss the Go-named PrimaryKey ("ID"), so on_conflict=update created a
// fresh row instead of updating the one named.
func recordPrimaryKey(record map[string]interface{}, mi datasource.ModelInfo) (interface{}, bool) {
	if v, ok := record[mi.PrimaryKey]; ok && v != nil && v != "" {
		return v, true
	}
	for _, f := range mi.Fields {
		if !f.IsPK {
			continue
		}
		for _, key := range fieldPayloadKeys(f, record) {
			if v := record[key]; v != nil && v != "" {
				return v, true
			}
		}
	}
	return nil, false
}

// dsFindFieldByColumn is the datasource.ModelInfo variant of findFieldByColumn:
// it matches a field by storage column or Go name and returns a pointer into
// the model's Fields slice (nil when unmatched).
func dsFindFieldByColumn(mi datasource.ModelInfo, column string) *datasource.FieldInfo {
	for i := range mi.Fields {
		if mi.Fields[i].Column == column || mi.Fields[i].Name == column {
			return &mi.Fields[i]
		}
	}
	return nil
}

// ImportFromFile handles the complete import flow: read file → parse → validate → import.
func (p *Panel) ImportFromFile(ctx context.Context, storageKey string, cfg ImportConfig) (*ImportReport, error) {
	if p.store == nil {
		return nil, fmt.Errorf("storage not configured")
	}

	reader, _, err := p.store.Get(ctx, storageKey)
	if err != nil {
		return nil, fmt.Errorf("read import file: %w", err)
	}
	defer reader.Close()

	// Parse
	records, err := ParseImportData(reader, cfg.Format)
	if err != nil {
		return nil, fmt.Errorf("parse import data: %w", err)
	}

	// Get model metadata
	mi, ok := p.src.Get(cfg.Model)
	if !ok {
		return nil, fmt.Errorf("model %q not found", cfg.Model)
	}

	// Validate
	errors := ValidateImportData(mi, records, cfg.TenantID)
	if len(errors) > 0 {
		// Return validation errors without importing
		report := &ImportReport{
			Total:  len(records),
			Failed: len(errors),
			Errors: errors,
			DryRun: true,
		}
		return report, nil
	}

	// Execute import
	return p.ExecuteImport(ctx, cfg, records)
}
