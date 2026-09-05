package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"

	"github.com/jcsvwinston/orbit/datasource"
)

// DjangoFixtureRecord represents a single record in Django-style fixture format.
// Format: {"model": "app.ModelName", "pk": 1, "fields": {...}}
type DjangoFixtureRecord struct {
	Model  string                 `json:"model"`
	PK     interface{}            `json:"pk"`
	Fields map[string]interface{} `json:"fields"`
}

// DumpdataConfig configures the dumpdata operation.
type DumpdataConfig struct {
	Models   []string `json:"models"`    // Models to export (empty = all)
	Database string   `json:"database"`  // Source database alias
	TenantID string   `json:"tenant_id"` // Tenant scope (empty = all)
}

// LoaddataConfig configures the loaddata operation.
type LoaddataConfig struct {
	StorageKey string `json:"key"`         // Storage key of the fixture file
	OnConflict string `json:"on_conflict"` // "skip" (default) or "update"
	Database   string `json:"database"`    // Target database alias
	TenantID   string `json:"tenant_id"`   // Tenant ID for auto-injection
}

// Dumpdata exports registered models to a Django-compatible JSON fixture file.
// Each record is serialized as {"model": "AppName.ModelName", "pk": <id>, "fields": {...}}.
func (p *Panel) Dumpdata(ctx context.Context, cfg DumpdataConfig) (ExportResult, error) {
	result := ExportResult{
		Status:    "processing",
		Format:    "django_fixture",
		CreatedAt: time.Now().UTC(),
	}

	if p.store == nil {
		return result, fmt.Errorf("storage not configured")
	}

	// Resolve models to export
	modelsToExport := cfg.Models
	if len(modelsToExport) == 0 {
		for _, m := range p.src.All() {
			modelsToExport = append(modelsToExport, m.Name)
		}
	}
	sort.Strings(modelsToExport)

	databaseAlias := cfg.Database
	if databaseAlias == "" {
		databaseAlias = p.defaultDBAlias
	}

	// Build fixture records in Django format
	fixtureRecords := make([]DjangoFixtureRecord, 0)
	totalRecords := 0

	for _, modelName := range modelsToExport {
		mi, ok := p.src.Get(modelName)
		if !ok {
			continue
		}

		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return result, fmt.Errorf("dumpdata model %s: %w", modelName, err)
		}

		// Build filters including tenant if applicable
		filters := make(map[string]string)
		if cfg.TenantID != "" && mi.TenantField != "" {
			filters[mi.TenantField] = cfg.TenantID
		}

		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 10000,
			Filters: filters,
		})
		if err != nil {
			return result, fmt.Errorf("dumpdata fetch %s: %w", modelName, err)
		}

		for _, item := range page.Items {
			// Extract PK value
			pkValue := recordPKValue(item, mi)

			// Build fields map (exclude PK from fields, it goes in "pk")
			fieldsMap := recordToFixtureFields(mi, item)

			// Model name in Django format: "app.ModelName"
			// We use just the model name since Go doesn't have app labels
			djangoModelName := modelName

			record := DjangoFixtureRecord{
				Model:  djangoModelName,
				PK:     pkValue,
				Fields: fieldsMap,
			}
			fixtureRecords = append(fixtureRecords, record)
			totalRecords++
		}
	}

	// Sort fixture records by model name, then by PK for deterministic output
	sort.SliceStable(fixtureRecords, func(i, j int) bool {
		if fixtureRecords[i].Model != fixtureRecords[j].Model {
			return fixtureRecords[i].Model < fixtureRecords[j].Model
		}
		return fmt.Sprintf("%v", fixtureRecords[i].PK) < fmt.Sprintf("%v", fixtureRecords[j].PK)
	})

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(fixtureRecords, "", "  ")
	if err != nil {
		return result, fmt.Errorf("dumpdata marshal fixture: %w", err)
	}

	// Store the fixture file
	ts := time.Now().UTC().Format("20060102150405")
	key := storage.CleanupTempKey("fixture") + fmt.Sprintf("_%s.json", ts)

	info, err := p.store.Put(ctx, key, bytes.NewReader(jsonData), storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: "application/json",
	})
	if err != nil {
		return result, fmt.Errorf("dumpdata store fixture: %w", err)
	}

	return finalizeExport(result, info, totalRecords, fmt.Sprintf("fixture_%s.json", ts), p.store, ctx)
}

// Loaddata imports data from a Django-compatible JSON fixture file.
// It auto-detects models from the "model" field, skips unknown models,
// and handles conflicts based on the OnConflict setting.
func (p *Panel) Loaddata(ctx context.Context, cfg LoaddataConfig) (*ImportReport, error) {
	if p.store == nil {
		return nil, fmt.Errorf("storage not configured")
	}

	// Default on_conflict to "skip"
	if cfg.OnConflict == "" {
		cfg.OnConflict = "skip"
	}
	if cfg.OnConflict != "skip" && cfg.OnConflict != "update" {
		return nil, fmt.Errorf("invalid on_conflict value: %q (must be \"skip\" or \"update\")", cfg.OnConflict)
	}

	databaseAlias := cfg.Database
	if databaseAlias == "" {
		databaseAlias = p.defaultDBAlias
	}

	// Read fixture file from storage
	reader, _, err := p.store.Get(ctx, cfg.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("loaddata read fixture: %w", err)
	}
	defer reader.Close()

	fixtureData, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("loaddata read fixture body: %w", err)
	}

	// Parse fixture records
	var fixtureRecords []DjangoFixtureRecord
	if err := json.Unmarshal(fixtureData, &fixtureRecords); err != nil {
		return nil, fmt.Errorf("loaddata parse fixture: %w", err)
	}

	report := &ImportReport{
		Total: len(fixtureRecords),
	}

	// Group records by model for efficient processing
	recordsByModel := make(map[string][]DjangoFixtureRecord)
	for _, rec := range fixtureRecords {
		modelName := rec.Model
		recordsByModel[modelName] = append(recordsByModel[modelName], rec)
	}

	// Process each model
	modelNames := make([]string, 0, len(recordsByModel))
	for name := range recordsByModel {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)

	for _, modelName := range modelNames {
		records := recordsByModel[modelName]

		mi, ok := p.src.Get(modelName)
		if !ok {
			// Skip records for models not in registry
			report.Skipped += len(records)
			continue
		}

		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return report, fmt.Errorf("loaddata model %s: %w", modelName, err)
		}

		// Process records for this model
		for _, rec := range records {
			// Merge fields with PK
			data := make(map[string]interface{})
			for k, v := range rec.Fields {
				data[k] = v
			}

			// The pk travels as the boundary string (ADR-001 D1): "7", 7 and
			// a UUID are all keys, and the backend narrows them. A pk with no
			// usable text is a failed row — it used to be dropped silently,
			// which created the record afresh under a new key.
			pkValue := ""
			if rec.PK != nil {
				normalized, err := normalizePKValue(rec.PK)
				if err != nil {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Message: fmt.Sprintf("model %s: invalid pk %v: %v", modelName, rec.PK, err),
					})
					continue
				}
				data[mi.PrimaryKey] = rec.PK
				pkValue = normalized
			} else {
				pkValue = extractDataPK(data, mi)
			}

			// A fixture loaded into a tenant belongs to it: a record naming
			// another tenant fails, one naming none gets the tenant stamped.
			if mi.TenantField != "" && cfg.TenantID != "" {
				if v, exists := data[mi.TenantField]; !exists {
					data[mi.TenantField] = cfg.TenantID
				} else if got, _ := canonicalID(v); got != cfg.TenantID {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Field:   mi.TenantField,
						Message: fmt.Sprintf("model %s pk=%s: tenant %q does not match the fixture's tenant %q", modelName, pkValue, got, cfg.TenantID),
					})
					continue
				}
			}

			// Determine if record already exists
			if pkValue == "" {
				// No PK, just create
				if _, err := st.Create(ctx, datasource.Record(data)); err != nil {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Message: fmt.Sprintf("model %s create: %v", modelName, err),
					})
				} else {
					report.Imported++
				}
				continue
			}

			// Check if record exists
			existing, err := st.Get(ctx, pkValue)
			if err != nil {
				// A key the backend refuses outright ("abc" on an integer
				// key) must not fall through to a create that would drop
				// the pk and store the row under a fresh key.
				if isClientError(err) {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Message: fmt.Sprintf("model %s pk=%s: %v", modelName, pkValue, err),
					})
					continue
				}
				// Record doesn't exist, create it
				if _, err := st.Create(ctx, datasource.Record(data)); err != nil {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Message: fmt.Sprintf("model %s create pk=%s: %v", modelName, pkValue, err),
					})
				} else {
					report.Imported++
				}
				continue
			}

			// Record exists, handle conflict
			if cfg.OnConflict == "skip" {
				report.Skipped++
				continue
			}

			// On-conflict: update
			if existing != nil {
				// Build updates from fields (exclude PK)
				updates := make(map[string]interface{})
				for k, v := range rec.Fields {
					field := dsFindFieldByColumn(mi, k)
					if field != nil && !field.IsPK && !field.IsReadOnly {
						updates[k] = v
					}
				}

				if err := st.Update(ctx, pkValue, datasource.Record(updates)); err != nil {
					report.Failed++
					report.Errors = append(report.Errors, ImportError{
						Message: fmt.Sprintf("model %s update pk=%s: %v", modelName, pkValue, err),
					})
				} else {
					report.Updated++
				}
			}
		}
	}

	return report, nil
}

// handleDumpdata is the HTTP handler for dumpdata.
// POST /api/fixtures/dumpdata
// Body: {"models": ["User", "Post"], "database": "default"}
// Returns export result with download URL.
func (p *Panel) handleDumpdata(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "export_data"); err != nil {
		return err
	}

	var cfg DumpdataConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}
	// A scoped request dumps its own tenant, whatever the body names.
	if scoped := p.enforcedTenantID(r); scoped != "" {
		cfg.TenantID = scoped
	}

	result, err := p.Dumpdata(r.Context(), cfg)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	}

	// Store result for status lookup
	if p.exportResults != nil {
		p.exportMu.Lock()
		result.ID = result.StorageKey
		p.exportResults[result.ID] = result
		p.exportMu.Unlock()
	}

	// Data leaving the system: audited whether it completed or failed.
	p.recordAuditEntry(r, AuditEntry{
		Action:    "fixtures.dumpdata",
		ModelName: "export",
		RecordID:  result.StorageKey,
		NewValue: map[string]any{
			"models":    cfg.Models,
			"format":    result.Format,
			"database":  cfg.Database,
			"tenant_id": cfg.TenantID,
			"status":    result.Status,
			"records":   result.Records,
			"size":      result.Size,
			"error":     result.Error,
		},
	})

	status := http.StatusOK
	if result.Status == "failed" {
		status = http.StatusInternalServerError
	}
	return c.JSON(status, result)
}

// handleLoaddata is the HTTP handler for loaddata.
// POST /api/fixtures/loaddata
// Body: {"key": "storage-key", "on_conflict": "skip"}
// Returns import report.
func (p *Panel) handleLoaddata(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "import_data"); err != nil {
		return err
	}

	var cfg LoaddataConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	if cfg.StorageKey == "" {
		return gferrors.BadRequest("key is required")
	}
	if scoped := p.enforcedTenantID(r); scoped != "" {
		cfg.TenantID = scoped
	}

	report, err := p.Loaddata(r.Context(), cfg)
	if err != nil {
		return fmt.Errorf("loaddata: %w", err)
	}

	entry := auditImportReportEntry("fixtures.loaddata", cfg.StorageKey, report)
	entry.NewValue["on_conflict"] = cfg.OnConflict
	entry.NewValue["database"] = cfg.Database
	entry.NewValue["tenant_id"] = cfg.TenantID
	p.recordAuditEntry(r, entry)

	return c.JSON(http.StatusOK, report)
}

// recordPKValue extracts the primary key value from a neutral Record. It is the
// datasource.ModelInfo replacement for extractPKValue over reflect.Value.
func recordPKValue(rec datasource.Record, mi datasource.ModelInfo) interface{} {
	for _, f := range mi.Fields {
		if f.IsPK {
			if v, ok := recordValue(rec, f); ok {
				return v
			}
		}
	}
	if v, ok := rec["id"]; ok {
		return v
	}
	return nil
}

// recordToFixtureFields converts a neutral Record to a map of field values for
// fixture export. Excludes the primary key field (it goes in the "pk" field of
// the fixture record). It is the datasource.ModelInfo replacement for
// entityToFixtureFields.
func recordToFixtureFields(mi datasource.ModelInfo, rec datasource.Record) map[string]interface{} {
	fieldsMap := make(map[string]interface{})
	for _, f := range mi.Fields {
		if f.IsExcluded || f.IsPK {
			continue
		}
		val, ok := recordValue(rec, f)
		if !ok {
			continue
		}
		fieldsMap[f.Column] = formatFixtureValue(val)
	}
	return fieldsMap
}

// extractDataPK reads the primary key from an input data map (fixture fields
// merged with pk), returning it as a string suitable for RecordStore's string
// IDs (D1). Returns "" when no usable PK value is present.
func extractDataPK(data map[string]interface{}, mi datasource.ModelInfo) string {
	if v, ok := data[mi.PrimaryKey]; ok && v != nil && v != "" {
		return fmt.Sprintf("%v", v)
	}
	for _, f := range mi.Fields {
		if !f.IsPK {
			continue
		}
		for _, key := range []string{f.Column, f.Name} {
			if key == "" {
				continue
			}
			if v, ok := data[key]; ok && v != nil && v != "" {
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

// formatFixtureValue formats a Go value for JSON fixture serialization.
func formatFixtureValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case time.Time:
		if val.IsZero() {
			return nil
		}
		return val.Format(time.RFC3339)
	case *time.Time:
		if val == nil || val.IsZero() {
			return nil
		}
		return val.Format(time.RFC3339)
	default:
		// Handle pointers to primitive types
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return nil
			}
			return formatFixtureValue(rv.Elem().Interface())
		}
		return v
	}
}

// normalizePKValue renders a fixture pk as the boundary string every
// RecordStore takes (ADR-001 D1) — a number, a numeric string and a UUID are
// all keys; the backend narrows them. nil and blank values are errors.
func normalizePKValue(raw interface{}) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("nil pk")
	}
	switch raw.(type) {
	case map[string]interface{}, []interface{}, bool:
		return "", fmt.Errorf("unsupported pk type: %T", raw)
	}
	id, ok := canonicalID(raw)
	if !ok {
		return "", fmt.Errorf("empty pk")
	}
	return id, nil
}

// isClientError reports whether err is the backend refusing the request
// itself (a 4xx domain error other than not found), as opposed to a row that
// does not exist or a backend failure.
func isClientError(err error) bool {
	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) {
		return false
	}
	return domErr.StatusCode >= 400 && domErr.StatusCode < 500 && domErr.StatusCode != http.StatusNotFound
}
