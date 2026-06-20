package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	gfdb "github.com/jcsvwinston/nucleus/pkg/db"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func (p *Panel) handleListMigrations(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "migration_view"); err != nil {
		return err
	}

	migrationsPath := p.migrationsPath()
	statuses, err := p.getMigrationStatus(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to list migrations: %w", err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":    true,
		"path":       migrationsPath,
		"mode":       p.migrationMode(),
		"migrations": statuses,
		"total":      len(statuses),
	})
}

func (p *Panel) handleApplyMigrations(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "migration_apply"); err != nil {
		return err
	}

	var req struct {
		Steps int `json:"steps"` // 0 = all pending
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	migrator := p.migrationRuntime()
	if migrator == nil {
		return gferrors.BadRequest("database runtime is not configured for migrations")
	}

	migrationsPath := p.migrationsPath()
	before, err := p.getMigrationStatus(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	pendingBefore := countPendingMigrations(before)
	requestedSteps := req.Steps
	steps := requestedSteps
	if steps <= 0 || steps > pendingBefore {
		steps = pendingBefore
	}

	if steps > 0 {
		if requestedSteps <= 0 {
			err = migrator.Up()
		} else {
			err = migrator.Steps(steps)
		}
		if err != nil {
			return fmt.Errorf("failed to apply migrations: %w", err)
		}
	}

	after, err := p.getMigrationStatus(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to refresh migration status: %w", err)
	}

	appliedIDs := appliedMigrationIDs(before, after)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"applied":         len(appliedIDs),
		"applied_ids":     appliedIDs,
		"pending":         countPendingMigrations(after),
		"requested_steps": requestedSteps,
		"executed_steps":  steps,
		"mode":            "runtime",
		"migrations":      after,
	})
}

func (p *Panel) migrationsPath() string {
	if p == nil || strings.TrimSpace(p.config.MigrationsPath) == "" {
		return "migrations"
	}
	return strings.TrimSpace(p.config.MigrationsPath)
}

func (p *Panel) migrationMode() string {
	if p != nil && p.db != nil {
		return "runtime"
	}
	return "inspect-only"
}

func (p *Panel) migrationRuntime() *gfdb.Migrator {
	if p == nil || p.db == nil {
		return nil
	}
	return gfdb.NewMigrator(p.db, p.migrationsPath(), p.logger)
}

func (p *Panel) getMigrationStatus(migrationsPath string) ([]migrationStatusInfo, error) {
	if migrator := p.migrationRuntime(); migrator != nil {
		statuses, err := migrator.Status()
		if err != nil {
			return nil, err
		}
		return toMigrationStatusInfo(statuses), nil
	}
	return inspectMigrationFiles(migrationsPath)
}

type migrationStatusInfo struct {
	ID        string `json:"id"`
	HasUp     bool   `json:"has_up"`
	HasDown   bool   `json:"has_down"`
	Applied   bool   `json:"applied"`
	AppliedAt string `json:"applied_at,omitempty"`
}

func inspectMigrationFiles(migrationsPath string) ([]migrationStatusInfo, error) {
	entries, err := os.ReadDir(migrationsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []migrationStatusInfo{}, nil
		}
		return nil, err
	}

	byID := map[string]*migrationStatusInfo{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		id, kind, ok := migrationFileParts(name)
		if !ok {
			continue
		}
		mig := byID[id]
		if mig == nil {
			mig = &migrationStatusInfo{ID: id}
			byID[id] = mig
		}
		if kind == "up" {
			mig.HasUp = true
		}
		if kind == "down" {
			mig.HasDown = true
		}
	}

	result := make([]migrationStatusInfo, 0, len(byID))
	for _, mig := range byID {
		result = append(result, *mig)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func toMigrationStatusInfo(statuses []gfdb.MigrationStatus) []migrationStatusInfo {
	rows := make([]migrationStatusInfo, 0, len(statuses))
	for _, status := range statuses {
		row := migrationStatusInfo{
			ID:      status.ID,
			HasUp:   status.HasUp,
			HasDown: status.HasDown,
			Applied: status.Applied,
		}
		if status.AppliedAt != nil {
			row.AppliedAt = status.AppliedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}

func migrationFileParts(name string) (id string, kind string, ok bool) {
	switch {
	case strings.HasSuffix(name, ".up.sql"):
		return strings.TrimSuffix(name, ".up.sql"), "up", true
	case strings.HasSuffix(name, ".down.sql"):
		return strings.TrimSuffix(name, ".down.sql"), "down", true
	default:
		return "", "", false
	}
}

func countPendingMigrations(statuses []migrationStatusInfo) int {
	total := 0
	for _, status := range statuses {
		if !status.Applied {
			total++
		}
	}
	return total
}

func appliedMigrationIDs(before, after []migrationStatusInfo) []string {
	beforeApplied := make(map[string]bool, len(before))
	for _, row := range before {
		beforeApplied[row.ID] = row.Applied
	}

	ids := make([]string, 0)
	for _, row := range after {
		if row.Applied && !beforeApplied[row.ID] {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
