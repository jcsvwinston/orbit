package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/orbit/server/routing"
	"github.com/jcsvwinston/orbit/server/services"
)

// auditExportLimit bounds one download. The window bounds the store; the
// ring is 2048 entries; a file of this many rows is what a browser and a
// spreadsheet handle without complaint.
const auditExportLimit = 10000

// auditExportHandler serves the fleet audit trail as a file:
// GET /api/audit/export?format=csv|json&limit=N. The trail comes from the
// store when the server retains (bounded by its window) and from the
// ring otherwise, newest first, the same rows ListAudit answers.
func auditExportHandler(state *services.State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
		if format == "" {
			format = "csv"
		}
		if format != "csv" && format != "json" {
			http.Error(w, "format must be csv or json", http.StatusBadRequest)
			return
		}
		limit := auditExportLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
				return
			}
			if n < limit {
				limit = n
			}
		}
		entries, err := auditRows(r.Context(), state, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stamp := time.Now().UTC().Format("20060102T150405Z")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fleet-audit-%s.%s"`, stamp, format))
		w.Header().Set("Cache-Control", "no-store")
		switch format {
		case "json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			type row struct {
				Time   time.Time `json:"time"`
				Actor  string    `json:"actor"`
				Action string    `json:"action"`
				Target string    `json:"target"`
				NodeID string    `json:"node_id"`
				Before string    `json:"before_json,omitempty"`
				After  string    `json:"after_json,omitempty"`
			}
			rows := make([]row, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, row{e.Time.UTC(), e.Actor, e.Action, e.Target, e.NodeID, e.Before, e.After})
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rows)
		default:
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			cw := csv.NewWriter(w)
			_ = cw.Write([]string{"time", "actor", "action", "target", "node_id", "before_json", "after_json"})
			for _, e := range entries {
				_ = cw.Write([]string{e.Time.UTC().Format(time.RFC3339Nano), e.Actor, e.Action, e.Target, e.NodeID, e.Before, e.After})
			}
			cw.Flush()
		}
	})
}

func auditRows(ctx context.Context, state *services.State, limit int) ([]routing.AuditEntry, error) {
	if state == nil {
		return nil, fmt.Errorf("admin server: state not initialized")
	}
	if state.Store == nil {
		if state.Audit == nil {
			return nil, fmt.Errorf("admin server: audit ring not initialized")
		}
		return state.Audit.List(limit), nil
	}
	if err := state.Store.Flush(ctx); err != nil {
		return nil, fmt.Errorf("admin server: audit store: %w", err)
	}
	rows, err := state.Store.ListAudit(limit)
	if err != nil {
		return nil, fmt.Errorf("admin server: audit store: %w", err)
	}
	out := make([]routing.AuditEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, routing.AuditEntry{Time: r.Time, Actor: r.Actor, Action: r.Action, Target: r.Target, NodeID: r.NodeID, Before: r.Before, After: r.After})
	}
	return out, nil
}
