package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

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

// ImportError describes a single row import error. Row is the row's
// position in the file, counted from 0.
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

// declaredBits is the width, in bits, of the Go numeric kind a column
// declares — the bitSize strconv asks for. Zero is how strconv spells "the
// platform's word size", which is what "int" and "uint" are.
func declaredBits(goType string) int {
	switch goType {
	case "int8", "uint8":
		return 8
	case "int16", "uint16":
		return 16
	case "int32", "uint32", "float32":
		return 32
	case "int64", "uint64", "float64":
		return 64
	}
	return 0
}

// validateFieldValue reports whether a cell holds a value of the type its
// column declares. It is what stands between a CSV and the writer: an import
// aborts on any validation error and otherwise hands the parsed records
// straight to Create, so a cell this function passes is a cell the store is
// asked to write.
//
// THE WIDTH IS PART OF THE TYPE. This parsed every integer kind at 64 bits,
// so "300" passed for an int8 column and "5000000000" for an int32 one:
// values no row of that table can hold, left for the driver to refuse halfway
// through an import or — on a driver that converts rather than refuses — to
// truncate into a different number than the file said. It is the same
// asymmetry the agent's parseID had, and it is fixed the same way, from the
// declared kind.
func validateFieldValue(field datasource.FieldInfo, value string) error {
	if value == "" {
		return nil
	}
	bits := declaredBits(field.GoType)
	switch field.GoType {
	case "int", "int8", "int16", "int32", "int64":
		if _, err := strconv.ParseInt(value, 10, bits); err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return fmt.Errorf("integer value out of range for %s: %q", field.GoType, value)
			}
			return fmt.Errorf("invalid integer value: %q", value)
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		// ParseUint permits no sign at all, which is the rule this column
		// wants: "-1" is not a small unsigned number, it is not one.
		if _, err := strconv.ParseUint(value, 10, bits); err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return fmt.Errorf("unsigned integer value out of range for %s: %q", field.GoType, value)
			}
			return fmt.Errorf("invalid unsigned integer value: %q", value)
		}
	case "float32", "float64":
		if _, err := strconv.ParseFloat(value, bits); err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return fmt.Errorf("float value out of range for %s: %q", field.GoType, value)
			}
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

// ExecuteImport writes already-parsed records into cfg.Model for c's
// operator: it plans the file (planImport), refusing it whole when one row is
// not one the operator may write, and then writes the plan — unless
// cfg.DryRun, which stops after the plan. It does not validate the cells:
// ImportFromFile does, between the two.
func (p *Panel) ExecuteImport(c *router.Context, cfg ImportConfig, records []map[string]interface{}) (*ImportReport, error) {
	mi, st, err := p.importTarget(cfg)
	if err != nil {
		return nil, err
	}
	plan, err := p.planImport(c, st, mi, cfg, records)
	if err != nil {
		return nil, err
	}
	report := &ImportReport{Total: len(records), DryRun: cfg.DryRun, Errors: make([]ImportError, 0)}
	if !cfg.DryRun {
		runImportWrites(c.Request.Context(), st, plan, report)
	}
	return report, nil
}

// importTarget resolves the model an import writes into and its store.
func (p *Panel) importTarget(cfg ImportConfig) (datasource.ModelInfo, datasource.RecordStore, error) {
	mi, ok := p.src.Get(cfg.Model)
	if !ok {
		return datasource.ModelInfo{}, nil, fmt.Errorf("model %q not found", cfg.Model)
	}
	st, err := p.src.Store(mi.Name, cfg.Database)
	if err != nil {
		return datasource.ModelInfo{}, nil, fmt.Errorf("get store for model %s: %w", cfg.Model, err)
	}
	return mi, st, nil
}

// What an import may write (OR-67).
//
// The import and the fixture load are granted by import_data on admin:*,
// which says who may import, not what. Until OR-67 that grant was the whole
// of it: whoever held it wrote any model, by create or by update, outside
// their own rows and through a field kept from them — what they could not
// write by hand. Each row of a file now asks what the record form asks
// (requestWriteScope): the model's create for a new row and its update for
// an existing one, the tenant, the operator's own rows under an #own grant,
// and no field the operator may not write. A file is planned whole before
// any row of it is written, and one row the operator may not write refuses
// it all (importRefused) — the way a read-only model refuses it (OR-65,
// OR-68). The validate step plans the file the same way, so it refuses what
// the execute step would.

// The verbs of a plan beyond the two a write scope is granted for.
const (
	// importSkip is an existing row on_conflict=skip leaves alone. It
	// writes nothing, so it asks for no grant.
	importSkip = "skip"
	// importFail is a row the plan already knows will fail for what it
	// holds (a fixture's unusable pk), not for who is writing it: it is a
	// failed row of the report, as it always was.
	importFail = "fail"
)

// importWrite is one row of a file as it will be written.
type importWrite struct {
	// row is the row's position in the file, from 0 (ImportError.Row).
	row int
	// action is fieldActionCreate, fieldActionUpdate, importSkip or
	// importFail.
	action string
	// id is the existing row an update or a skip names.
	id string
	// data is what is written: the whole row on a create, the columns it
	// changes on an update.
	data map[string]any
	// label names the row in a failure the fixture load reports ("model
	// Post pk=7"); the import names it by row alone.
	label string
	// failure is the failed row an importFail reports.
	failure ImportError
}

// failed is the report's entry for a write of w the store refused.
func (w importWrite) failed(err error) ImportError {
	if w.label == "" {
		return ImportError{Row: w.row, Message: fmt.Sprintf("%s failed: %v", w.action, err)}
	}
	return ImportError{Row: w.row, Message: fmt.Sprintf("%s %s: %v", w.label, w.action, err)}
}

// importRefused is the 403 an import or a fixture load answers when one row
// of the file is not one the operator may write. It names the row — counted
// from 1, as a person counts the records of a file — the model and why, and
// it comes before any row of the file is written.
func importRefused(mi datasource.ModelInfo, row int, cause error) error {
	why := cause.Error()
	return &gferrors.DomainError{
		Code:       "PERMISSION_DENIED",
		Message:    fmt.Sprintf("nothing in the file was written: row %d (%s): %s", row+1, mi.Name, why),
		StatusCode: http.StatusForbidden,
		Details:    map[string]any{"row": row + 1, "model": mi.Name, "reason": why},
	}
}

// notInTenant is why a file may not name an existing row of another tenant:
// the record endpoints answer such a row as not found, and so does this.
func notInTenant(mi datasource.ModelInfo, id string, scope tenantScope) error {
	return fmt.Errorf("%s %s not found in tenant %q", mi.Name, id, scope.Tenant)
}

// admitWrite holds one planned write to the scope the operator's grant for
// its verb reaches (writeScopes): the grant itself, the row an update names
// — rec, as the store returned it — and the payload. It returns why the
// write is refused, or the store's error when the question could not be
// answered.
func admitWrite(ctx context.Context, scopes *writeScopes, st datasource.RecordStore, mi datasource.ModelInfo, w *importWrite, rec datasource.Record) (refusal, err error) {
	if w.action != fieldActionCreate && w.action != fieldActionUpdate {
		return nil, nil
	}
	scope, refusal := scopes.get(w.action)
	if refusal != nil {
		return refusal, nil
	}
	if w.action == fieldActionUpdate && scope.confined() {
		reached, err := scope.reaches(ctx, st, mi, w.id, rec)
		if err != nil {
			return nil, err
		}
		if !reached {
			return fmt.Errorf("%s %s not found among the rows this operator may update", mi.Name, w.id), nil
		}
	}
	return scope.guardPayload(mi, w.data), nil
}

// planImport decides, before any row is written, what each row of an import
// does, and refuses the whole file with importRefused when one of them is
// not a write this operator may make.
//
// A row is EXISTING when on_conflict asks to look for it (skip or update)
// and its primary key — under the model's pk name, column or Go name —
// names a row the store holds, or else every column of one of the model's
// unique indexes is in the row and matches a row of the import's tenant. An
// existing row of another tenant is not found whatever on_conflict says,
// since a skip would confirm it exists. An existing row is skipped, which
// writes nothing and asks nothing, or updated: the model's update, a row the
// operator reaches (their tenant's, their own under an #own grant) and the
// columns the row changes. Every other row is a create: the model's create
// and the whole row, the tenant and the owner stamped as the record form
// stamps them.
func (p *Panel) planImport(c *router.Context, st datasource.RecordStore, mi datasource.ModelInfo, cfg ImportConfig, records []map[string]interface{}) ([]importWrite, error) {
	ctx := c.Request.Context()
	// The tenant the import targets: the request's for a scoped request
	// (the handlers force it), the body's tenant_id for an unscoped one. A
	// row naming another tenant is refused, one naming none is stamped.
	target := p.importTenantScope(mi, cfg.TenantID)
	scopes := p.writeScopesFor(c, mi)
	lookup := cfg.OnConflict == "skip" || cfg.OnConflict == "update"

	plan := make([]importWrite, 0, len(records))
	for i, record := range records {
		w := importWrite{row: i, action: fieldActionCreate, data: maps.Clone(record)}
		if target.Enforced() {
			if err := target.guardPayload(w.data, true); err != nil {
				return nil, importRefused(mi, i, err)
			}
		}
		var existing datasource.Record
		if lookup {
			if id, rec, err := p.findExistingByUniqueFields(ctx, st, mi, w.data, target); err == nil && id != "" {
				if target.Enforced() {
					owned, err := target.owns(ctx, st, mi, id, rec)
					if err != nil {
						return nil, err
					}
					if !owned {
						return nil, importRefused(mi, i, notInTenant(mi, id, target))
					}
				}
				w.id, existing = id, rec
				if cfg.OnConflict == "skip" {
					w.action = importSkip
				} else {
					w.action = fieldActionUpdate
					w.data = importUpdates(mi, w.data)
				}
			}
		}
		refusal, err := admitWrite(ctx, scopes, st, mi, &w, existing)
		if err != nil {
			return nil, err
		}
		if refusal != nil {
			return nil, importRefused(mi, i, refusal)
		}
		plan = append(plan, w)
	}
	return plan, nil
}

// importUpdates is what an update of an existing row writes: the row's
// columns of the model, but its primary key and its read-only fields.
func importUpdates(mi datasource.ModelInfo, record map[string]interface{}) map[string]interface{} {
	updates := make(map[string]interface{}, len(record))
	for col, val := range record {
		if col == "_model" {
			continue
		}
		field := dsFindFieldByColumn(mi, col)
		if field != nil && !field.IsPK && !field.IsReadOnly {
			updates[col] = val
		}
	}
	return updates
}

// runImportWrites writes a plan, row by row, into report. A row the store
// refuses is a failed row of the report — the backends have no transaction
// to roll the file back with (ADR-001) — but no row the operator may not
// write reaches this: the plan refused the file before.
func runImportWrites(ctx context.Context, st datasource.RecordStore, plan []importWrite, report *ImportReport) {
	for _, w := range plan {
		var err error
		switch w.action {
		case importFail:
			report.Failed++
			report.Errors = append(report.Errors, w.failure)
			continue
		case importSkip:
			report.Skipped++
			continue
		case fieldActionUpdate:
			if err = st.Update(ctx, w.id, datasource.Record(w.data)); err == nil {
				report.Updated++
				continue
			}
		default:
			if _, err = st.Create(ctx, datasource.Record(w.data)); err == nil {
				report.Imported++
				continue
			}
		}
		report.Failed++
		report.Errors = append(report.Errors, w.failed(err))
	}
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

// ImportFromFile is the import of an uploaded file for c's operator, the
// validate step (cfg.DryRun) and the execute step alike: read and parse the
// file, plan it (planImport), which refuses it whole when one row is not one
// the operator may write, validate its cells, and write the plan unless the
// cells fail or cfg.DryRun. Both steps take the same path up to the write,
// so the validate step refuses what the execute step would.
func (p *Panel) ImportFromFile(c *router.Context, storageKey string, cfg ImportConfig) (*ImportReport, error) {
	if p.store == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	ctx := c.Request.Context()

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

	mi, st, err := p.importTarget(cfg)
	if err != nil {
		return nil, err
	}

	// Who may write what comes first: a file the operator may not write is
	// refused before its cells are read for anybody's benefit.
	plan, err := p.planImport(c, st, mi, cfg, records)
	if err != nil {
		return nil, err
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

	report := &ImportReport{Total: len(records), DryRun: cfg.DryRun, Errors: make([]ImportError, 0)}
	if !cfg.DryRun {
		runImportWrites(ctx, st, plan, report)
	}
	return report, nil
}
