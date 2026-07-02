package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Health check API handlers

type healthCheckResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // healthy, degraded, unhealthy
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type healthSummary struct {
	Status    string              `json:"status"`
	CheckedAt string              `json:"checked_at"`
	Checks    []healthCheckResult `json:"checks"`
	Uptime    string              `json:"uptime"`
	Version   string              `json:"version"`
}

func (p *Panel) handleHealthCheck(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "health_check"); err != nil {
		return err
	}

	checks := make([]healthCheckResult, 0)
	overallStatus := "healthy"

	// Database health
	for _, dbInfo := range p.config.Databases {
		alias := dbInfo.Alias
		handle, err := p.databaseHandle(alias)
		if err != nil {
			checks = append(checks, healthCheckResult{
				Name:    "db:" + alias,
				Status:  "unhealthy",
				Message: err.Error(),
			})
			overallStatus = "unhealthy"
			continue
		}

		start := time.Now()
		sqlDB, sqlErr := handle.SqlDB()
		if sqlErr != nil {
			checks = append(checks, healthCheckResult{
				Name:    "db:" + alias,
				Status:  "unhealthy",
				Message: sqlErr.Error(),
			})
			overallStatus = "unhealthy"
			continue
		}

		if err := sqlDB.Ping(); err != nil {
			checks = append(checks, healthCheckResult{
				Name:      "db:" + alias,
				Status:    "unhealthy",
				Message:   err.Error(),
				LatencyMS: time.Since(start).Milliseconds(),
			})
			overallStatus = "unhealthy"
		} else {
			checks = append(checks, healthCheckResult{
				Name:      "db:" + alias,
				Status:    "healthy",
				Message:   "connected",
				LatencyMS: time.Since(start).Milliseconds(),
			})
		}
	}

	// Redis health (if configured)
	redisCheck := inspectRedisRuntime(context.Background(), p.config.RedisURL)
	if redisCheck.Enabled {
		checks = append(checks, healthCheckResult{
			Name:      "redis",
			Status:    redisCheck.Status,
			Message:   redisCheck.Message,
			LatencyMS: redisCheck.LatencyMS,
		})
		switch redisCheck.Status {
		case "unhealthy":
			overallStatus = "unhealthy"
		case "degraded":
			if overallStatus == "healthy" {
				overallStatus = "degraded"
			}
		}
	}

	return c.JSON(http.StatusOK, healthSummary{
		Status:    overallStatus,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
		Version:   "Nucleus admin",
	})
}

// Job queue detail handlers

func (p *Panel) handleListJobQueues(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "jobs_view"); err != nil {
		return err
	}

	// Return job queue info from tasks runtime
	snapshot := tasks.RuntimeSnapshot{}
	if p.config.TaskInspector != nil {
		snapshot = p.config.TaskInspector.InspectRuntime()
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":   p.config.RedisURL != "",
		"redis_url": p.config.RedisURL,
		"snapshot":  snapshot,
	})
}

// Multi-site management API handlers

func (p *Panel) handleListSites(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "sites_view"); err != nil {
		return err
	}

	if !p.config.MultiSiteEnabled {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled": false,
			"reason":  "Multi-site not enabled",
			"sites":   []interface{}{},
		})
	}

	sites := make([]siteInfo, 0)
	for _, name := range p.config.MultiSiteNames {
		sites = append(sites, siteInfo{
			Name:    name,
			Default: name == p.config.MultiSiteDefault,
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled": true,
		"default": p.config.MultiSiteDefault,
		"sites":   sites,
		"total":   len(sites),
	})
}

type siteInfo struct {
	Name        string   `json:"name"`
	Hosts       []string `json:"hosts,omitempty"`
	Database    string   `json:"database,omitempty"`
	Default     bool     `json:"is_default"`
	TenantCount int      `json:"tenant_count,omitempty"`
}

// Export/Import API handlers (Data Studio integration)

func (p *Panel) handleExportCreate(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "export_data"); err != nil {
		return err
	}

	var cfg ExportConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	if cfg.Format == "" {
		cfg.Format = ExportFormatCSV
	}

	result, err := p.exportModels(r.Context(), cfg)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	}

	// Store result for status lookup
	if p.exportResults != nil {
		p.exportMu.Lock()
		result.ID = result.StorageKey // Use storage key as ID
		p.exportResults[result.ID] = result
		p.exportMu.Unlock()
	}

	status := http.StatusOK
	if result.Status == "failed" {
		status = http.StatusInternalServerError
	}
	return c.JSON(status, result)
}

func (p *Panel) handleExportList(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "export_data"); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p.listExportJobs())
}

func (p *Panel) handleExportStatus(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "export_data"); err != nil {
		return err
	}
	id := c.Param("id")
	if id == "" {
		id = c.Query("id")
	}

	result, ok := p.getExportJob(id)
	if !ok {
		return gferrors.NotFound("export", id)
	}
	return c.JSON(http.StatusOK, result)
}

func (p *Panel) handleExportDownload(c *router.Context) error {
	w, r := c.Writer, c.Request
	if err := p.authorizeAction(c, "*", "export_data"); err != nil {
		return err
	}

	key := c.Query("key")
	if key == "" {
		return gferrors.BadRequest("key query parameter is required")
	}

	if p.store == nil {
		return gferrors.BadRequest("storage not configured")
	}

	reader, info, err := p.store.Get(r.Context(), key)
	if err != nil {
		return err
	}
	defer reader.Close()

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Key))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	io.Copy(w, reader)
	return nil
}

func (p *Panel) handleImportValidate(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "import_data"); err != nil {
		return err
	}

	// Read upload into temp storage key
	key := c.Query("key")
	if key == "" {
		return gferrors.BadRequest("key query parameter is required")
	}

	var cfg ImportConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	mi, ok := p.src.Get(cfg.Model)
	if !ok {
		return gferrors.BadRequest(fmt.Sprintf("model %q not found", cfg.Model))
	}
	cfg.Model = mi.Name

	if p.store == nil {
		return gferrors.BadRequest("storage not configured")
	}

	// Run the shared import flow in dry-run mode: it reads the upload, parses,
	// and validates without writing (ExecuteImport short-circuits on DryRun).
	cfg.DryRun = true
	report, err := p.ImportFromFile(r.Context(), key, cfg)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"total_records": report.Total,
		"valid_records": report.Total - len(report.Errors),
		"errors":        report.Errors,
		"can_proceed":   len(report.Errors) == 0,
	})
}

func (p *Panel) handleImportExecute(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "import_data"); err != nil {
		return err
	}

	key := c.Query("key")
	if key == "" {
		return gferrors.BadRequest("key query parameter is required")
	}

	var cfg ImportConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	// Get tenant from context if not specified
	if cfg.TenantID == "" {
		if tenantCtx := tenantContextFromRequest(r); tenantCtx != nil {
			cfg.TenantID = tenantCtx.TenantID
		}
	}

	report, err := p.ImportFromFile(r.Context(), key, cfg)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, report)
}

func (p *Panel) handleImportUpload(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "import_data"); err != nil {
		return err
	}

	if p.store == nil {
		return gferrors.BadRequest("storage not configured")
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50MB max
		return gferrors.BadRequest("file too large (max 50MB)")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return gferrors.BadRequest("file is required")
	}
	defer file.Close()

	// Determine format from extension
	format := "csv"
	if strings.HasSuffix(strings.ToLower(header.Filename), ".json") {
		format = "json"
	} else if strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		format = "csv"
	}

	// Store uploaded file temporarily
	key := storage.CleanupTempKey("import") + "_" + header.Filename
	info, err := p.store.Put(r.Context(), key, file, storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: header.Header.Get("Content-Type"),
	})
	if err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"key":      info.Key,
		"size":     info.Size,
		"format":   format,
		"filename": header.Filename,
	})
}
