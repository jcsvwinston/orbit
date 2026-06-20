package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
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
func ValidateImportConfig(cfg ImportConfig, registry *model.Registry) error {
	if cfg.Model == "" {
		return fmt.Errorf("model is required")
	}
	if _, ok := registry.Get(cfg.Model); !ok {
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
func ValidateImportData(meta *model.ModelMeta, records []map[string]interface{}, tenantID string) []ImportError {
	errors := make([]ImportError, 0)

	for rowIdx, record := range records {
		// Check _model field for multi-model exports
		if modelField, ok := record["_model"]; ok && modelField != nil {
			if mf, ok := modelField.(string); ok && mf != "" && mf != meta.Name {
				// This record is for a different model
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Message: fmt.Sprintf("record belongs to model %q, expected %q", mf, meta.Name),
				})
				continue
			}
		}

		// Validate fields
		for col, val := range record {
			if col == "_model" {
				continue
			}

			field := findFieldByColumn(meta, col)
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
			if err := validateFieldValue(field, strVal); err != nil {
				errors = append(errors, ImportError{
					Row:     rowIdx,
					Field:   col,
					Message: err.Error(),
				})
			}
		}

		// Tenant field check
		tenantField := meta.TenantFieldName()
		if tenantField != "" && tenantID != "" {
			// Tenant will be auto-injected, no validation needed
		}
	}

	return errors
}

func findFieldByColumn(meta *model.ModelMeta, column string) *model.FieldMeta {
	for i := range meta.Fields {
		if meta.Fields[i].Column == column || meta.Fields[i].Name == column {
			return &meta.Fields[i]
		}
	}
	return nil
}

func validateFieldValue(field *model.FieldMeta, value string) error {
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
	meta, ok := p.registry.Get(cfg.Model)
	if !ok {
		return nil, fmt.Errorf("model %q not found", cfg.Model)
	}

	crud, err := p.getCRUD(meta, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("get CRUD for model %s: %w", cfg.Model, err)
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

	for rowIdx, record := range records {
		// Auto-inject tenant ID
		tenantField := meta.TenantFieldName()
		if tenantField != "" && cfg.TenantID != "" {
			if _, exists := record[tenantField]; !exists {
				// Also check Go field name
				goField := columnToGoField(meta, tenantField)
				if goField != "" {
					if _, exists2 := record[goField]; !exists2 {
						record[tenantField] = cfg.TenantID
					}
				} else {
					record[tenantField] = cfg.TenantID
				}
			}
		}

		// Convert to entity
		entity, err := payloadToEntity(meta, record)
		if err != nil {
			report.Failed++
			report.Errors = append(report.Errors, ImportError{
				Row:     rowIdx,
				Message: fmt.Sprintf("failed to convert record: %v", err),
			})
			continue
		}

		// Check for existing record (for on_conflict handling)
		if cfg.OnConflict == "skip" || cfg.OnConflict == "update" {
			existing, err := p.findExistingByUniqueFields(ctx, crud, meta, record)
			if err == nil && existing != nil {
				if cfg.OnConflict == "skip" {
					report.Skipped++
					continue
				}
				if cfg.OnConflict == "update" {
					// Update existing
					existingID := getEntityID(existing, meta)
					if existingID != 0 {
						updates := make(map[string]interface{})
						for col, val := range record {
							if col == "_model" {
								continue
							}
							field := findFieldByColumn(meta, col)
							if field != nil && !field.IsPK && !field.IsReadOnly {
								updates[col] = val
							}
						}
						if err := crud.Update(ctx, existingID, updates); err != nil {
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
		}

		// Create new record
		if err := crud.Create(ctx, entity); err != nil {
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

func (p *Panel) findExistingByUniqueFields(ctx context.Context, crud model.CRUDOperator, meta *model.ModelMeta, record map[string]interface{}) (interface{}, error) {
	// Try to find by primary key first
	pkField := meta.PrimaryKey
	pkVal, ok := record[pkField]
	if ok && pkVal != nil && pkVal != "" {
		id, err := strconv.ParseUint(fmt.Sprintf("%v", pkVal), 10, 64)
		if err == nil {
			return crud.FindByID(ctx, uint(id))
		}
	}

	// Try to find by unique indexes
	for _, idx := range meta.Indexes {
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

		// Query with unique index filter
		result, err := crud.FindAll(ctx, model.QueryOpts{
			Page: 1, PageSize: 1, Filters: filters,
		})
		if err == nil && result.Total > 0 {
			items := reflect.ValueOf(result.Items)
			if items.Kind() == reflect.Ptr {
				items = items.Elem()
			}
			if items.Len() > 0 {
				return items.Index(0).Addr().Interface(), nil
			}
		}
	}

	return nil, fmt.Errorf("not found")
}

func getEntityID(entity interface{}, meta *model.ModelMeta) uint {
	if entity == nil {
		return 0
	}
	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	pkField := val.FieldByName(meta.PrimaryKey)
	if pkField.IsValid() {
		switch v := pkField.Interface().(type) {
		case uint:
			return v
		case uint64:
			return uint(v)
		case int:
			return uint(v)
		case int64:
			return uint(v)
		}
	}
	return 0
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
	meta, ok := p.registry.Get(cfg.Model)
	if !ok {
		return nil, fmt.Errorf("model %q not found", cfg.Model)
	}

	// Validate
	errors := ValidateImportData(meta, records, cfg.TenantID)
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
