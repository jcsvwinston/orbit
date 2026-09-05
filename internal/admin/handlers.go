package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// handleListModels returns all registered models with their record counts.
func (p *Panel) handleListModels(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "list_models"); err != nil {
		return err
	}
	includeCounts := includeModelCounts(r)

	type modelInfo struct {
		Name        string           `json:"name"`
		Plural      string           `json:"plural"`
		Table       string           `json:"table"`
		Icon        string           `json:"icon"`
		Count       int64            `json:"count"`
		CountKnown  bool             `json:"count_known"`
		IsEstimated bool             `json:"is_estimated"`
		Counts      map[string]int64 `json:"counts,omitempty"`
		Databases   []string         `json:"databases,omitempty"`
		Database    string           `json:"database"`
		Engine      string           `json:"engine"`
	}
	type runtimeModelInfo struct {
		Name        string `json:"name"`
		Plural      string `json:"plural"`
		Table       string `json:"table"`
		Count       int64  `json:"count"`
		CountKnown  bool   `json:"count_known"`
		IsEstimated bool   `json:"is_estimated"`
	}
	type runtimeDatabaseInfo struct {
		Alias        string             `json:"alias"`
		Engine       string             `json:"engine"`
		Dialect      string             `json:"dialect"`
		IsDefault    bool               `json:"is_default"`
		Models       []string           `json:"models"`
		ModelEntries []runtimeModelInfo `json:"model_entries"`
		ModelCount   int                `json:"model_count"`
	}
	type runtimeEngineInfo struct {
		Name      string                `json:"name"`
		Databases []runtimeDatabaseInfo `json:"databases"`
	}
	type runtimeInfo struct {
		Environment      string                `json:"environment"`
		Databases        []runtimeDatabaseInfo `json:"databases"`
		Engines          []string              `json:"engines"`
		EngineGroups     []runtimeEngineInfo   `json:"engine_groups"`
		TraceURLTemplate string                `json:"trace_url_template,omitempty"`
		ModelsTotal      int                   `json:"models_total"`
		RecordsTotal     int64                 `json:"records_total"`
		CountsMode       string                `json:"counts_mode"`
		CountsAvailable  bool                  `json:"counts_available"`
		SessionsCount    int                   `json:"sessions_active"`

		// Multi-tenant/site info
		MultiTenantEnabled bool     `json:"multi_tenant_enabled"`
		MultiTenantDefault string   `json:"multi_tenant_default"`
		TenantIDs          []string `json:"tenant_ids,omitempty"`
		MultiSiteEnabled   bool     `json:"multi_site_enabled"`
		MultiSiteDefault   string   `json:"multi_site_default"`
		SiteNames          []string `json:"site_names,omitempty"`
	}

	models := p.src.All()
	result := make([]modelInfo, 0, len(models))
	for _, m := range models {
		count := int64(0)
		if !includeCounts {
			count = -1
		}
		info := modelInfo{
			Name:       m.Name,
			Plural:     m.Plural,
			Table:      m.Table,
			Icon:       m.Icon,
			Count:      count,
			CountKnown: false,
			Counts:     map[string]int64{},
			// Filled from probed table PRESENCE per alias below (both count
			// modes), so multi-database topologies (e.g. tenant-isolated
			// schemas) attribute each model to the databases that really
			// hold its table — not just the declared alias. Falls back to
			// the declared/default alias when nothing is probed.
			Databases: []string{},
			Database:  m.DatabaseAlias,
		}
		if info.Database == "" {
			info.Database = "default"
		}
		if dbInfo, ok := p.databaseRuntimeInfoByAlias(info.Database); ok {
			info.Engine = dbInfo.Dialect
			if info.Engine == "" {
				info.Engine = dbInfo.Engine
			}
		}
		result = append(result, info)
	}
	// Sort BEFORE indexing by pointer: modelByName holds pointers into
	// result, and sorting after taking them left each pointer aimed at a
	// POSITION of the slice instead of a model — with a non-alphabetical
	// registration order, every count/database written through the map
	// landed on the wrong model (e.g. Article's count shown on Author).
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	modelByName := make(map[string]*modelInfo, len(result))
	for i := range result {
		modelByName[result[i].Name] = &result[i]
	}

	aliases := p.sortedDatabaseAliases()
	dbRuntime := make([]runtimeDatabaseInfo, 0, len(aliases))
	engineGroups := map[string][]runtimeDatabaseInfo{}
	enginesSeen := map[string]struct{}{}
	modelRecordsByAlias := map[string]map[string]int64{}
	defaultAlias := p.defaultDBAlias

	for _, alias := range aliases {
		cfg, ok := p.databaseRuntimeInfoByAlias(alias)
		if !ok {
			cfg = DatabaseRuntimeInfo{
				Alias:     alias,
				Engine:    "",
				Dialect:   "",
				IsDefault: alias == p.defaultDBAlias,
			}
		}

		modelNames := make([]string, 0, len(models))
		modelEntries := make([]runtimeModelInfo, 0, len(models))
		records := map[string]int64{}
		queryable := true
		if _, err := p.resolveDatabaseAlias(alias); err != nil {
			queryable = false
		}

		if includeCounts {
			if queryable {
				for _, m := range models {
					st, served, err := p.storeOnAlias(m, alias)
					if err != nil {
						return fmt.Errorf("admin.ListModels store alias=%s model=%s: %w", alias, m.Name, err)
					}
					if !served {
						continue
					}
					cr, err := st.Count(r.Context())
					if err != nil {
						return fmt.Errorf("admin.ListModels count alias=%s model=%s: %w", alias, m.Name, err)
					}
					count, estimated, present := cr.Count, cr.IsEstimated, cr.Present
					if !present {
						continue
					}
					records[m.Name] = count
					modelNames = append(modelNames, m.Name)
					modelEntries = append(modelEntries, runtimeModelInfo{
						Name:        m.Name,
						Plural:      m.Plural,
						Table:       m.Table,
						Count:       count,
						CountKnown:  true,
						IsEstimated: estimated,
					})

					if mi, ok := modelByName[m.Name]; ok {
						if alias == defaultAlias || (mi.Count == 0 && !mi.CountKnown) {
							mi.Count = count
							mi.CountKnown = true
							mi.IsEstimated = estimated
						}
						mi.Counts[alias] = count

						// Add database alias if not already present
						found := false
						for _, dbName := range mi.Databases {
							if dbName == alias {
								found = true
								break
							}
						}
						if !found {
							mi.Databases = append(mi.Databases, alias)
						}
					}
				}
			}
		} else {
			if queryable {
				for _, m := range models {
					// Fast mode still probes table PRESENCE (a zero-row
					// scan), so database attribution stays truthful without
					// paying for counts.
					st, served, err := p.storeOnAlias(m, alias)
					if err != nil {
						return fmt.Errorf("admin.ListModels store alias=%s model=%s: %w", alias, m.Name, err)
					}
					if !served || !st.TableExists(r.Context()) {
						continue
					}
					modelNames = append(modelNames, m.Name)
					records[m.Name] = -1
					modelEntries = append(modelEntries, runtimeModelInfo{
						Name:       m.Name,
						Plural:     m.Plural,
						Table:      m.Table,
						Count:      -1,
						CountKnown: false,
					})
					if mi, ok := modelByName[m.Name]; ok {
						found := false
						for _, dbName := range mi.Databases {
							if dbName == alias {
								found = true
								break
							}
						}
						if !found {
							mi.Databases = append(mi.Databases, alias)
						}
					}
				}
			}
		}
		sort.Strings(modelNames)
		sort.SliceStable(modelEntries, func(i, j int) bool {
			return modelEntries[i].Name < modelEntries[j].Name
		})
		modelRecordsByAlias[alias] = records

		dbInfo := runtimeDatabaseInfo{
			Alias:        cfg.Alias,
			Engine:       cfg.Engine,
			Dialect:      cfg.Dialect,
			IsDefault:    cfg.IsDefault,
			Models:       modelNames,
			ModelEntries: modelEntries,
			ModelCount:   len(modelNames),
		}
		dbRuntime = append(dbRuntime, dbInfo)

		engineLabel := strings.TrimSpace(cfg.Dialect)
		if engineLabel == "" {
			engineLabel = strings.TrimSpace(cfg.Engine)
		}
		if engineLabel == "" {
			engineLabel = "unknown"
		}
		enginesSeen[engineLabel] = struct{}{}
		engineGroups[engineLabel] = append(engineGroups[engineLabel], dbInfo)
	}

	var recordsTotal int64
	for _, m := range models {
		row := modelByName[m.Name]
		if row == nil {
			continue
		}
		if includeCounts {
			recordsTotal += row.Count
		}
		// Presence probing found no home (unqueryable handles, missing
		// tables): fall back to the declared/default alias so the model is
		// never attributed to zero databases.
		if len(row.Databases) == 0 {
			row.Databases = []string{row.Database}
		}
		sort.Strings(row.Databases)
	}

	engines := make([]string, 0, len(enginesSeen))
	for label := range enginesSeen {
		engines = append(engines, label)
	}
	sort.Strings(engines)

	engineRuntime := make([]runtimeEngineInfo, 0, len(engines))
	for _, engine := range engines {
		rows := engineGroups[engine]
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].IsDefault != rows[j].IsDefault {
				return rows[i].IsDefault
			}
			return rows[i].Alias < rows[j].Alias
		})
		engineRuntime = append(engineRuntime, runtimeEngineInfo{
			Name:      engine,
			Databases: rows,
		})
	}

	sessionsCount := 0
	if p.config.Session != nil {
		if payloads, supported, err := allSessionPayloads(r.Context(), p.config.Session); err == nil && supported {
			sessionsCount = len(payloads)
		}
	}

	countsMode := "full"
	if !includeCounts {
		countsMode = "light"
		recordsTotal = -1
	}

	totalModelsAcrossDBs := 0
	for _, dbInfo := range dbRuntime {
		totalModelsAcrossDBs += dbInfo.ModelCount
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"models": result,
		"title":  p.config.Title,
		"runtime": runtimeInfo{
			Environment:        strings.TrimSpace(p.config.Environment),
			Databases:          dbRuntime,
			Engines:            engines,
			EngineGroups:       engineRuntime,
			TraceURLTemplate:   strings.TrimSpace(p.config.TraceURLTemplate),
			ModelsTotal:        totalModelsAcrossDBs,
			RecordsTotal:       recordsTotal,
			CountsMode:         countsMode,
			CountsAvailable:    includeCounts,
			SessionsCount:      sessionsCount,
			MultiTenantEnabled: p.config.MultiTenantEnabled,
			MultiTenantDefault: p.config.MultiTenantDefault,
			TenantIDs:          p.config.MultiTenantIDs,
			MultiSiteEnabled:   p.config.MultiSiteEnabled,
			MultiSiteDefault:   p.config.MultiSiteDefault,
			SiteNames:          p.config.MultiSiteNames,
		},
	})
}

// handleGetSchema returns metadata for a specific model.
func (p *Panel) handleGetSchema(c *router.Context) error {
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "get_schema"); err != nil {
		return err
	}

	type fieldInfo struct {
		Name          string              `json:"name"`
		Column        string              `json:"column"`
		Label         string              `json:"label"`
		Type          string              `json:"type"`
		HTMLType      string              `json:"html_type"`
		IsPK          bool                `json:"is_pk"`
		IsRequired    bool                `json:"is_required"`
		IsReadOnly    bool                `json:"is_readonly"`
		IsList        bool                `json:"is_list"`
		IsSearch      bool                `json:"is_search"`
		IsFilter      bool                `json:"is_filter"`
		IsExcluded    bool                `json:"is_excluded"`
		IsForeignKey  bool                `json:"is_fk"`
		IsTenantField bool                `json:"is_tenant_field"`
		ForeignModel  string              `json:"fk_model,omitempty"`
		Choices       []datasource.Choice `json:"choices,omitempty"`
	}

	fields := make([]fieldInfo, 0, len(mi.Fields))
	for _, f := range mi.Fields {
		if f.IsExcluded {
			continue
		}
		fields = append(fields, fieldInfo{
			Name: f.Name, Column: f.Column, Label: f.Label,
			Type: f.GoType, HTMLType: f.HTMLType,
			IsPK: f.IsPK, IsRequired: f.IsRequired, IsReadOnly: f.IsReadOnly,
			IsList: f.IsList, IsSearch: f.IsSearch, IsFilter: f.IsFilter,
			IsExcluded: f.IsExcluded, IsForeignKey: f.IsForeignKey,
			IsTenantField: f.IsTenantField,
			ForeignModel:  f.ForeignModel, Choices: f.Choices,
		})
	}

	tenantField := p.resolveTenantField(mi.Name)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"name":         mi.Name,
		"plural":       mi.Plural,
		"table":        mi.Table,
		"primary_key":  mi.PrimaryKey,
		"icon":         mi.Icon,
		"read_only":    mi.ReadOnly,
		"fields":       fields,
		"foreign_keys": mi.ForeignKeys,
		"tenant_field": tenantField,
	})
}

// handleUpdateFieldMeta updates field metadata properties at runtime (like Django ModelAdmin).
func (p *Panel) handleUpdateFieldMeta(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	// Runtime field-meta edits are a Nucleus registry feature. A panel
	// mounted on a custom DataSource (Config.DataSource) has no
	// SchemaRegistry; answer 501 instead of dereferencing nil (which
	// panicked into a 500 before authorization even ran).
	if p.registry == nil {
		return &gferrors.DomainError{
			Code:       "NOT_IMPLEMENTED",
			Message:    "field metadata updates are not available for this data source",
			StatusCode: http.StatusNotImplemented,
		}
	}
	meta, ok := p.registry.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, meta.Name, "update_schema"); err != nil {
		return err
	}

	var payload struct {
		Fields map[string]model.FieldMetaUpdate `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	if len(payload.Fields) == 0 {
		return gferrors.BadRequest("no field updates provided")
	}

	// Snapshot the touched fields before and after, so the audit entry
	// carries the schema change itself (a generic "update of <model>" with
	// no record and no values told nothing).
	before := auditFieldMetaSnapshot(meta, payload.Fields)
	if err := p.registry.BulkUpdateFieldMeta(name, payload.Fields); err != nil {
		return gferrors.BadRequest(err.Error())
	}
	var after map[string]any
	if updated, ok := p.registry.Get(name); ok {
		after = auditFieldMetaSnapshot(updated, payload.Fields)
	}
	p.recordAuditEntry(r, AuditEntry{
		Action:    "schema.update",
		ModelName: meta.Name,
		OldValue:  before,
		NewValue:  after,
	})

	return c.JSON(http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": fmt.Sprintf("Updated %d field(s) for %s", len(payload.Fields), name),
	})
}

// handleListRecords returns a paginated list of records for a model.
func (p *Panel) handleListRecords(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "list"); err != nil {
		return err
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}
	page, pageSet, err := parsePositiveQueryInt(r.URL.Query(), "page")
	if err != nil {
		return err
	}
	pageSize, pageSizeSet, err := parsePositiveQueryInt(r.URL.Query(), "page_size")
	if err != nil {
		return err
	}
	if pageSizeSet && pageSize > 200 {
		return gferrors.BadRequest("page_size must be <= 200")
	}

	search, err := sanitizeSearchQuery(r.URL.Query().Get("search"))
	if err != nil {
		return err
	}
	// A model with nothing to search in cannot honour ?search=: the backends
	// drop the text and answer every row, which reads as "no match found"
	// while showing everything. Say so instead. ModelInfo is rebuilt from
	// the live registry, so enabling is_search in Field settings lifts this
	// without a restart.
	if search != "" && !modelSearchable(mi) {
		return gferrors.BadRequest(fmt.Sprintf("search is not available for %s: it has no searchable fields (tag them admin:\"search\", set ModelConfig.SearchFields, or enable is_search in Field settings)", mi.Name))
	}

	orderBy, err := dsSanitizeOrderBy(mi, r.URL.Query().Get("order_by"))
	if err != nil {
		return err
	}

	filters, err := dsCollectFilters(mi, r.URL.Query())
	if err != nil {
		return err
	}

	// Confine the list to the request's tenant when multi-tenant is enabled.
	if scope := p.requestTenantScope(r, mi); scope.Enforced() {
		if filters == nil {
			filters = make(map[string]string)
		}
		filters[scope.Column()] = scope.Tenant
	}

	if !pageSet {
		page = 0
	}
	if !pageSizeSet {
		pageSize = 0
	}

	result, err := st.List(r.Context(), datasource.Query{
		Page: page, PageSize: pageSize, Search: search,
		Filters: filters, OrderBy: orderBy,
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// handleGetRecord returns a single record by ID.
func (p *Panel) handleGetRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "retrieve"); err != nil {
		return err
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}
	record, err := scopedRecord(r.Context(), st, mi, idStr, p.requestTenantScope(r, mi))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, record)
}

// handleCreateRecord creates a new record.
func (p *Panel) handleCreateRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "create"); err != nil {
		return err
	}
	if mi.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	// A scoped request creates in its own tenant: a payload naming another
	// one (under any spelling of the column) is refused, a payload naming
	// none gets the tenant stamped.
	if scope := p.requestTenantScope(r, mi); scope.Enforced() {
		if err := scope.guardPayload(data, true); err != nil {
			return err
		}
	}

	created, err := st.Create(r.Context(), datasource.Record(data))
	if err != nil {
		return err
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "create",
		ModelName: mi.Name,
		RecordID:  auditRecordID(mi, created),
		NewValue:  auditValues(mi, created),
	})

	return c.JSON(http.StatusCreated, created)
}

// handleUpdateRecord updates an existing record.
func (p *Panel) handleUpdateRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "update"); err != nil {
		return err
	}
	if mi.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}
	// A scoped request only reaches rows of its tenant (another tenant's
	// row is not found) and cannot move a row to another tenant.
	if scope := p.requestTenantScope(r, mi); scope.Enforced() {
		if _, err := scopedRecord(r.Context(), st, mi, idStr, scope); err != nil {
			return err
		}
		if err := scope.guardPayload(updates, false); err != nil {
			return err
		}
	}
	// The row before and after the change go into the audit entry. A
	// failed read leaves that side nil but never turns a valid write into
	// an error: the update is the operation, the snapshot is its record.
	before := auditRecordSnapshot(r, st, idStr)
	if err := st.Update(r.Context(), idStr, datasource.Record(updates)); err != nil {
		return err
	}
	after := auditRecordSnapshot(r, st, idStr)
	p.recordAuditEntry(r, AuditEntry{
		Action:    "update",
		ModelName: mi.Name,
		RecordID:  idStr,
		OldValue:  auditValues(mi, before),
		NewValue:  auditValues(mi, after),
	})

	return c.JSON(http.StatusOK, map[string]interface{}{"updated": true, "id": idStr})
}

// handleDeleteRecord deletes a record by ID.
func (p *Panel) handleDeleteRecord(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	idStr := c.Param("id")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	if err := p.authorizeAction(c, mi.Name, "delete"); err != nil {
		return err
	}
	if mi.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}
	if scope := p.requestTenantScope(r, mi); scope.Enforced() {
		if _, err := scopedRecord(r.Context(), st, mi, idStr, scope); err != nil {
			return err
		}
	}
	before := auditRecordSnapshot(r, st, idStr)
	if err := st.Delete(r.Context(), idStr); err != nil {
		return err
	}
	p.recordAuditEntry(r, AuditEntry{
		Action:    "delete",
		ModelName: mi.Name,
		RecordID:  idStr,
		OldValue:  auditValues(mi, before),
	})

	return c.JSON(http.StatusOK, map[string]interface{}{"deleted": true, "id": idStr})
}

// handleBulkAction processes bulk operations (delete, export).
func (p *Panel) handleBulkAction(c *router.Context) error {
	r := c.Request
	name := c.Param("name")
	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}

	// Ids are strings at the boundary (ADR-001 D1): a UUID key is as valid
	// as an integer one, so the request carries them as raw JSON tokens
	// and decodeRecordIDs accepts strings and numbers alike.
	var req struct {
		Action string            `json:"action"`
		IDs    []json.RawMessage `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}
	ids, err := decodeRecordIDs(req.IDs)
	if err != nil {
		return err
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	// Fallback to model's declared database if no explicit override provided in query
	if r.URL.Query().Get("db") == "" && r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		if mi.DatabaseAlias != "" {
			databaseAlias = mi.DatabaseAlias
		}
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "delete":
		if err := p.authorizeAction(c, mi.Name, "bulk_delete"); err != nil {
			return err
		}
		if mi.ReadOnly {
			return gferrors.Forbidden("model is read-only")
		}
		if len(ids) == 0 {
			return gferrors.BadRequest("ids are required for delete action")
		}
		st, err := p.src.Store(mi.Name, databaseAlias)
		if err != nil {
			return err
		}

		type bulkDeleteError struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		}

		// An id the backend cannot narrow ("abc" on an integer key) or a row
		// of another tenant is a per-id failure in errors[], not a failed
		// request: that is the bulk contract the SPA reports from.
		scope := p.requestTenantScope(r, mi)
		deleted := 0
		failures := make([]bulkDeleteError, 0)
		deletedIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			if scope.Enforced() {
				if _, err := scopedRecord(r.Context(), st, mi, id, scope); err != nil {
					failures = append(failures, bulkDeleteError{ID: id, Error: err.Error()})
					continue
				}
			}
			// Each row that goes is audited as its own delete, with the
			// values it had, so "who removed record N" has the same answer
			// whether N went alone or in a batch.
			before := auditRecordSnapshot(r, st, id)
			deleteErr := st.Delete(r.Context(), id)
			if deleteErr == nil {
				deleted++
				deletedIDs = append(deletedIDs, id)
				p.recordAuditEntry(r, AuditEntry{
					Action:    "delete",
					ModelName: mi.Name,
					RecordID:  id,
					OldValue:  auditValues(mi, before),
				})
				continue
			}
			failures = append(failures, bulkDeleteError{
				ID:    id,
				Error: deleteErr.Error(),
			})
		}
		p.recordAuditEntry(r, AuditEntry{
			Action:    "bulk_delete",
			ModelName: mi.Name,
			NewValue: map[string]any{
				"requested": len(ids),
				"deleted":   deleted,
				"failed":    len(failures),
				"ids":       deletedIDs,
			},
		})
		return c.JSON(http.StatusOK, map[string]interface{}{
			"action":    "delete",
			"requested": len(ids),
			"deleted":   deleted,
			"failed":    len(failures),
			"errors":    failures,
		})

	case "export":
		if err := p.authorizeAction(c, mi.Name, "bulk_export"); err != nil {
			return err
		}
		if len(ids) == 0 {
			return gferrors.BadRequest("ids are required for export action")
		}
		// The export URL carries the selection as a comma-separated ?ids=
		// list, so a key containing a comma cannot be expressed in it.
		for i, id := range ids {
			if strings.Contains(id, ",") {
				return gferrors.BadRequest(fmt.Sprintf("ids[%d] contains a comma and cannot be selected for export", i))
			}
		}
		p.recordAuditEntry(r, AuditEntry{
			Action:    "bulk_export",
			ModelName: mi.Name,
			NewValue: map[string]any{
				"count":    len(ids),
				"ids":      ids,
				"database": databaseAlias,
			},
		})
		return c.JSON(http.StatusOK, map[string]interface{}{
			"export_url": buildBulkExportURL(r.URL.Path, ids, databaseAlias),
			"ids":        ids,
		})

	default:
		return gferrors.BadRequest("unknown action: " + req.Action)
	}
}

// auditRecordSnapshot reads one record for an audit entry's before/after
// value. A read failure yields nil: the snapshot documents the write, it
// must never decide whether the write happens.
func auditRecordSnapshot(r *http.Request, st datasource.RecordStore, id string) datasource.Record {
	if st == nil || r == nil {
		return nil
	}
	rec, err := st.Get(r.Context(), id)
	if err != nil {
		return nil
	}
	return rec
}

// auditFieldMetaSnapshot captures the runtime-editable properties of the
// fields that updates names (matched by Go name or column, as the registry
// matches them), keyed by the name the caller used.
func auditFieldMetaSnapshot(meta *model.ModelMeta, updates map[string]model.FieldMetaUpdate) map[string]any {
	if meta == nil || len(updates) == 0 {
		return nil
	}
	out := make(map[string]any, len(updates))
	for key := range updates {
		for i := range meta.Fields {
			f := &meta.Fields[i]
			if f.Name != key && f.Column != key {
				continue
			}
			out[key] = map[string]any{
				"label":       f.Label,
				"html_type":   f.HTMLType,
				"is_list":     f.IsList,
				"is_search":   f.IsSearch,
				"is_filter":   f.IsFilter,
				"is_excluded": f.IsExcluded,
				"is_readonly": f.IsReadOnly,
			}
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	gferrors.WriteError(w, r, err, nil)
}

// authErrorToDomain converts an AdminAuth.Authenticate failure into the
// client-facing 401. The client always sees a fixed "authentication
// required" message: the raw provider error can carry internal detail
// (DB connectivity, internal state, secrets embedded in a DSN) and must
// never leak to an unauthenticated caller. The raw error is logged
// server-side at Debug for diagnostics. Hardening from the ADR-016 review.
func (p *Panel) authErrorToDomain(err error) error {
	if err != nil {
		// Log via the panel logger when available, else the default —
		// never silently drop the diagnostic (matches warnAdminAuthDisabled).
		lg := slog.Default()
		if p != nil && p.logger != nil {
			lg = p.logger
		}
		lg.Debug("admin authentication failed", "error", err.Error())
	}
	return gferrors.Unauthorized("authentication required")
}

func authDeniedDomain(modelName, action string) error {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "*"
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "unknown"
	}
	return gferrors.Forbidden(fmt.Sprintf("not authorized to %s on %s", action, modelName))
}

func includeModelCounts(r *http.Request) bool {
	if r == nil {
		return true
	}
	stats := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("stats")))
	switch stats {
	case "light", "lite", "meta", "fast", "no-counts", "nocounts":
		return false
	case "full", "counts":
		return true
	}

	counts := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("counts")))
	switch counts {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	return true
}

func (p *Panel) databaseRuntimeInfoByAlias(alias string) (DatabaseRuntimeInfo, bool) {
	needle := strings.TrimSpace(alias)
	if needle == "" {
		return DatabaseRuntimeInfo{}, false
	}
	for _, item := range p.config.Databases {
		if strings.TrimSpace(item.Alias) == needle {
			return item, true
		}
	}
	return DatabaseRuntimeInfo{}, false
}

func parsePositiveQueryInt(values url.Values, key string) (value int, provided bool, err error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return 0, false, nil
	}

	n, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 1, true, nil // Be lenient with invalid inputs
	}
	if n <= 0 {
		return 1, true, nil // Normalize to 1 for pagination
	}
	return n, true, nil
}

func sanitizeSearchQuery(raw string) (string, error) {
	search := strings.TrimSpace(raw)
	if len(search) > 256 {
		return "", gferrors.BadRequest("search is too long (max 256 characters)")
	}
	return search, nil
}

func runtimeColumn(col string) string {
	if col == "i_d" {
		return "id"
	}
	return col
}

func buildBulkExportURL(currentPath string, ids []string, databaseAlias string) string {
	base := strings.TrimSuffix(currentPath, "/bulk")
	if base == currentPath {
		base = strings.TrimSuffix(currentPath, "/")
	}

	q := url.Values{}
	q.Set("ids", strings.Join(ids, ","))
	if alias := strings.TrimSpace(databaseAlias); alias != "" {
		q.Set("db", alias)
	}
	return base + "/export?" + q.Encode()
}

// storeOnAlias resolves the record store of model m on a database alias
// during the presence sweep of ListModels, which probes EVERY alias the app
// serves so the "Databases" column stays truthful. A data source that does
// not serve an alias (quarkdatasource is bound to exactly one, and since
// v1.8.18 says so instead of silently answering with the wrong database)
// is not an error there: the model is simply not present on that alias —
// the same outcome as a table that does not exist. An error on the model's
// OWN alias (its declared one, or the default) still propagates: that one
// the panel cannot explain away.
func (p *Panel) storeOnAlias(m datasource.ModelInfo, alias string) (datasource.RecordStore, bool, error) {
	st, err := p.src.Store(m.Name, alias)
	if err == nil {
		return st, true, nil
	}
	own := m.DatabaseAlias
	if own == "" {
		own = p.defaultDBAlias
	}
	if alias == own {
		return nil, false, err
	}
	return nil, false, nil
}
