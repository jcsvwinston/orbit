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
	Models   []string `json:"models"`    // Models to export (empty = every one the operator may list)
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

// Dumpdata exports the targets (exportTargets) to a Django-compatible JSON
// fixture file. Each record is serialized as {"model": "AppName.ModelName",
// "pk": <id>, "fields": {...}}, and carries the rows and fields its target's
// read scope lets the requesting operator see; a model with no target is not
// dumped.
func (p *Panel) Dumpdata(ctx context.Context, cfg DumpdataConfig, targets []exportTarget) (ExportResult, error) {
	result := ExportResult{
		Status:    "processing",
		Format:    "django_fixture",
		CreatedAt: time.Now().UTC(),
	}

	if p.store == nil {
		return result, fmt.Errorf("storage not configured")
	}

	databaseAlias := cfg.Database
	if databaseAlias == "" {
		databaseAlias = p.defaultDBAlias
	}

	// Build fixture records in Django format
	fixtureRecords := make([]DjangoFixtureRecord, 0)
	totalRecords := 0

	for _, t := range targets {
		mi := t.model
		modelName := mi.Name

		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return result, fmt.Errorf("dumpdata model %s: %w", modelName, err)
		}

		page, err := st.List(ctx, datasource.Query{
			Page: 1, PageSize: 10000,
			Filters: t.filters,
		})
		if err != nil {
			return result, fmt.Errorf("dumpdata fetch %s: %w", modelName, err)
		}

		for _, item := range page.Items {
			// Extract PK value
			pkValue := recordPKValue(item, mi)

			// Build fields map (exclude PK from fields, it goes in "pk"),
			// with only the fields this operator may read.
			fieldsMap := recordToFixtureFields(mi, t.read, item)

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
	key := exportKey("fixture", "json")

	info, err := p.store.Put(ctx, key, bytes.NewReader(jsonData), storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: "application/json",
	})
	if err != nil {
		return result, fmt.Errorf("dumpdata store fixture: %w", err)
	}

	return finalizeExport(result, info, totalRecords, fmt.Sprintf("fixture_%s.json", ts), p.store, ctx)
}

// Loaddata imports data from a Django-compatible JSON fixture file for c's
// operator. It auto-detects models from the "model" field, skips unknown
// models, and handles conflicts based on the OnConflict setting.
//
// It is the import in another format, and writes the way the import does
// (OR-67): every record of the fixture is planned before any is written —
// a record whose pk the store holds is an update (or a skip), any other a
// create — and each asks what the record form asks (planLoad). One record
// the operator may not write refuses the whole fixture with importRefused.
func (p *Panel) Loaddata(c *router.Context, cfg LoaddataConfig) (*ImportReport, error) {
	if p.store == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	ctx := c.Request.Context()

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

	// Group records by model for efficient processing, each with its
	// position in the file: a refusal names the record by it.
	recordsByModel := make(map[string][]fixtureRow)
	for i, rec := range fixtureRecords {
		modelName := rec.Model
		recordsByModel[modelName] = append(recordsByModel[modelName], fixtureRow{pos: i, rec: rec})
	}

	// Process each model
	modelNames := make([]string, 0, len(recordsByModel))
	for name := range recordsByModel {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)

	// A load writes rows, and a read-only model refuses every write the
	// panel makes — a create, an update, a delete, a batch, an import
	// (OR-65). A fixture that names one is refused whole, before any row
	// of it is written: half a fixture loaded is worse than none (OR-68).
	for _, modelName := range modelNames {
		if mi, ok := p.src.Get(modelName); ok && mi.ReadOnly {
			return nil, gferrors.Forbidden(fmt.Sprintf("model %s is read-only", mi.Name))
		}
	}

	// Every model is planned before any is written, for the same reason:
	// a record the operator may not write refuses the fixture whole.
	type modelPlan struct {
		st   datasource.RecordStore
		plan []importWrite
	}
	plans := make([]modelPlan, 0, len(modelNames))
	for _, modelName := range modelNames {
		rows := recordsByModel[modelName]

		mi, ok := p.src.Get(modelName)
		if !ok {
			// Skip records for models not in registry
			report.Skipped += len(rows)
			continue
		}

		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return report, fmt.Errorf("loaddata model %s: %w", modelName, err)
		}
		plan, err := p.planLoad(c, st, mi, cfg, rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, modelPlan{st: st, plan: plan})
	}

	for _, mp := range plans {
		runImportWrites(ctx, mp.st, mp.plan, report)
	}
	return report, nil
}

// fixtureRow is one record of a fixture and its position in the file.
type fixtureRow struct {
	pos int
	rec DjangoFixtureRecord
}

// planLoad is planImport for the records of one model of a fixture. A
// record is EXISTING when its pk names a row the store holds: it is skipped
// or updated (on_conflict), and any other record is a create. A pk with no
// usable text, or one the store refuses outright, is a failed record, as it
// always was; neither is a question of who is writing.
func (p *Panel) planLoad(c *router.Context, st datasource.RecordStore, mi datasource.ModelInfo, cfg LoaddataConfig, rows []fixtureRow) ([]importWrite, error) {
	ctx := c.Request.Context()
	modelName := mi.Name
	target := p.importTenantScope(mi, cfg.TenantID)
	scopes := p.writeScopesFor(c, mi)

	plan := make([]importWrite, 0, len(rows))
	for _, row := range rows {
		rec := row.rec
		// Merge fields with PK
		data := make(map[string]interface{})
		for k, v := range rec.Fields {
			data[k] = v
		}
		w := importWrite{row: row.pos, action: fieldActionCreate, data: data, label: "model " + modelName}

		// The pk travels as the boundary string (ADR-001 D1): "7", 7 and
		// a UUID are all keys, and the backend narrows them. A pk with no
		// usable text is a failed row — it used to be dropped silently,
		// which created the record afresh under a new key.
		pkValue := ""
		if rec.PK != nil {
			normalized, err := normalizePKValue(rec.PK)
			if err != nil {
				w.action = importFail
				w.failure = ImportError{Row: row.pos, Message: fmt.Sprintf("model %s: invalid pk %v: %v", modelName, rec.PK, err)}
				plan = append(plan, w)
				continue
			}
			data[mi.PrimaryKey] = rec.PK
			pkValue = normalized
		} else {
			pkValue = extractDataPK(data, mi)
		}
		if pkValue != "" {
			w.label = fmt.Sprintf("model %s pk=%s", modelName, pkValue)
		}

		// A fixture loaded into a tenant belongs to it: a record naming
		// another tenant (under any spelling of the column) is refused, one
		// naming none gets the tenant stamped.
		if target.Enforced() {
			if err := target.guardPayload(data, true); err != nil {
				return nil, importRefused(mi, row.pos, err)
			}
		}

		var existing datasource.Record
		if pkValue != "" {
			found, err := st.Get(ctx, pkValue)
			switch {
			case err != nil && isClientError(err):
				// A key the backend refuses outright ("abc" on an integer
				// key) must not fall through to a create that would drop
				// the pk and store the row under a fresh key.
				w.action = importFail
				w.failure = ImportError{Row: row.pos, Message: fmt.Sprintf("model %s pk=%s: %v", modelName, pkValue, err)}
				plan = append(plan, w)
				continue
			case err == nil:
				// The row the pk names must be the tenant's: a fixture can
				// neither update another tenant's row (it used to overwrite
				// and re-tenant it) nor, under on_conflict=skip, confirm it
				// exists. Reported as not found, the same answer a get
				// gives, so the id space of other tenants is not disclosed.
				// A record that carries no tenant key (the field is hidden
				// from JSON) is confirmed through the store rather than
				// taken for another tenant's.
				if target.Enforced() {
					owned, err := target.owns(ctx, st, mi, pkValue, found)
					if err != nil {
						return nil, err
					}
					if !owned {
						return nil, importRefused(mi, row.pos, notInTenant(mi, pkValue, target))
					}
				}
				w.id, existing = pkValue, found
				if cfg.OnConflict == "skip" {
					w.action = importSkip
				} else {
					// Build updates from fields (exclude PK)
					w.action = fieldActionUpdate
					w.data = importUpdates(mi, rec.Fields)
				}
			}
			// Any other error is a row the store does not hold: a create.
		}

		refusal, err := admitWrite(ctx, scopes, st, mi, &w, existing)
		if err != nil {
			return nil, err
		}
		if refusal != nil {
			return nil, importRefused(mi, row.pos, refusal)
		}
		plan = append(plan, w)
	}
	return plan, nil
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
	// The dump rides on export_data and is an export: each model carries
	// what this operator may list of it, as the panel's export does (OR-66).
	targets, err := p.exportTargets(c, cfg.Models, nil, cfg.TenantID)
	if err != nil {
		return err
	}

	result, err := p.Dumpdata(r.Context(), cfg, targets)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	}
	result.Tenant = cfg.TenantID
	result.producer = p.exportProducerOf(r)

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

	// import_data says who may load a fixture, not what: each record asks
	// what the record form asks, and one that is refused refuses the
	// fixture before any record is written (OR-67).
	report, err := p.Loaddata(c, cfg)
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
// fixture export: the fields read lets the operator see. Excludes the primary
// key field (it goes in the "pk" field of the fixture record). It is the
// datasource.ModelInfo replacement for entityToFixtureFields.
func recordToFixtureFields(mi datasource.ModelInfo, read readScope, rec datasource.Record) map[string]interface{} {
	fieldsMap := make(map[string]interface{})
	for _, f := range read.readableFields(mi) {
		if f.IsPK {
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
