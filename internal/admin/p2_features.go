package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// Deployment detection API handlers

type deploymentInfo struct {
	Runtime      string            `json:"runtime"` // standalone, docker, kubernetes
	Host         string            `json:"host"`
	Pod          string            `json:"pod,omitempty"`
	Instance     string            `json:"instance,omitempty"`
	NodeID       string            `json:"node_id"`
	Environment  string            `json:"environment"`
	StartedAt    string            `json:"started_at"`
	Uptime       string            `json:"uptime"`
	GoVersion    string            `json:"go_version"`
	GOOS         string            `json:"go_os"`
	GOARCH       string            `json:"go_arch"`
	GOMAXPROCS   int               `json:"gomaxprocs"`
	ClusterMode  bool              `json:"cluster_mode"`
	ClusterNodes []clusterNodeInfo `json:"cluster_nodes,omitempty"`
}

type clusterNodeInfo struct {
	NodeID   string `json:"node_id"`
	LastSeen string `json:"last_seen"`
	Status   string `json:"status"`
}

func (p *Panel) handleDeploymentInfo(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "system_view"); err != nil {
		return err
	}

	runtimeLabel := classifyRuntime(p.config.SessionRuntime)

	info := deploymentInfo{
		Runtime:     runtimeLabel,
		Host:        strings.TrimSpace(p.config.SessionRuntime.Host),
		Pod:         strings.TrimSpace(p.config.SessionRuntime.Pod),
		Instance:    strings.TrimSpace(p.config.SessionRuntime.Instance),
		NodeID:      p.liveNodeID(),
		Environment: strings.TrimSpace(p.config.Environment),
		GoVersion:   runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GOMAXPROCS:  runtime.GOMAXPROCS(0),
		ClusterMode: p.config.LiveClusterEnabled,
	}

	// Add cluster nodes if enabled
	if p.config.LiveClusterEnabled {
		info.ClusterNodes = p.getClusterNodes()
	}

	return c.JSON(http.StatusOK, info)
}

func (p *Panel) getClusterNodes() []clusterNodeInfo {
	if p.live == nil {
		return nil
	}

	nodes := p.live.nodes.active(liveNodeOnlineWindow)
	result := make([]clusterNodeInfo, 0, len(nodes))
	for id, lastSeen := range nodes {
		status := "online"
		if time.Since(lastSeen) > liveNodeDegradedWindow {
			status = "degraded"
		}
		result = append(result, clusterNodeInfo{
			NodeID:   id,
			LastSeen: lastSeen.Format(time.RFC3339),
			Status:   status,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].NodeID < result[j].NodeID
	})
	return result
}

// Cache management API handlers

func (p *Panel) handleCacheStats(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "cache_view"); err != nil {
		return err
	}

	snapshot := inspectCacheRuntime(r.Context(), p.config.Cache, p.config.RedisURL)
	payload := map[string]interface{}{
		"enabled":       snapshot.Enabled,
		"kind":          snapshot.Kind,
		"name":          snapshot.Name,
		"status":        snapshot.Status,
		"message":       snapshot.Message,
		"can_flush":     snapshot.CanFlush,
		"entries":       snapshot.Entries,
		"entries_known": snapshot.EntriesKnown,
		"redis_url":     snapshot.RedisURL,
		"latency_ms":    snapshot.LatencyMS,
		// key_count is what this view answered before a cache could be
		// anything but Redis. Kept as an alias of entries so a client
		// written against the old payload keeps working.
		"key_count": snapshot.Entries,
	}
	return c.JSON(http.StatusOK, payload)
}

func (p *Panel) handleFlushCache(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "cache_manage"); err != nil {
		return err
	}

	result, err := flushCacheRuntime(r.Context(), p.config.Cache, p.config.RedisURL)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}

	// Emptying a cache is a destructive act on live traffic, so it is
	// audited whichever cache it hit. A Redis URL is recorded with its
	// password masked: the trail is readable by anyone with audit_view.
	entry := map[string]any{
		"kind":          result.Kind,
		"name":          result.Name,
		"removed":       result.Removed,
		"removed_known": result.RemovedKnown,
	}
	if result.Kind == "redis" {
		entry["redis_url"] = redactURLCredentials(p.config.RedisURL)
	}
	p.recordAuditEntry(r, AuditEntry{
		Action:    "cache.flush",
		ModelName: "cache",
		NewValue:  entry,
	})

	payload := map[string]interface{}{
		"flushed":       result.Flushed,
		"kind":          result.Kind,
		"name":          result.Name,
		"message":       result.Message,
		"removed":       result.Removed,
		"removed_known": result.RemovedKnown,
	}
	if result.Kind == "redis" {
		// The shape this endpoint has always answered, kept beside the
		// new one so a client written against it keeps working.
		payload["redis_url"] = result.RedisURL
		payload["status"] = result.Status
		payload["latency_ms"] = result.LatencyMS
		payload["key_count_before"] = result.KeyCountBefore
		payload["key_count_after"] = result.KeyCountAfter
	}
	return c.JSON(http.StatusOK, payload)
}

// File storage browser API handlers

type storageFileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

func (p *Panel) handleListStorage(c *router.Context) error {
	r := c.Request
	storagePath := c.Query("path")
	if err := p.authorizeAction(c, "*", "storage_view"); err != nil {
		return err
	}

	if p.store != nil {
		// Confine the caller-supplied path to the browse root BEFORE it
		// reaches the store. This was the store branch's only missing
		// gate: normalizeStorageBrowsePath existed and was tested, but
		// nothing called it, so store.List got the raw query and a
		// storage_view session could enumerate any bucket prefix
		// (TestListStorageConfinesPathToRoot). The filesystem branch
		// below already confined with its own Abs+HasPrefix check.
		confined, err := normalizeStorageBrowsePath(storagePath)
		if err != nil {
			return gferrors.Forbidden(err.Error())
		}

		files, err := listConfiguredStorage(r.Context(), p.store, confined)
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"backend": "store",
			"path":    confined,
			"files":   files,
			"total":   len(files),
		})
	}

	relativePath := strings.TrimPrefix(storagePath, adminStorageBrowseRoot)
	relativePath = strings.TrimPrefix(relativePath, "/")

	absRoot, err := filepath.Abs(adminStorageBrowseRoot)
	if err != nil {
		return gferrors.InternalError("resolve storage root")
	}
	absPath := filepath.Join(absRoot, filepath.FromSlash(relativePath))

	if !strings.HasPrefix(absPath, absRoot) {
		return gferrors.Forbidden("access denied: path outside storage root")
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return gferrors.NotFound("storage path", storagePath)
	}

	files := make([]storageFileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, storageFileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(storagePath, entry.Name()),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}

	sortStorageEntries(files)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"backend": "filesystem",
		"path":    storagePath,
		"files":   files,
		"total":   len(files),
	})
}

// Email stats API handlers

func (p *Panel) handleEmailStats(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "email_view"); err != nil {
		return err
	}

	snapshot := inspectEmailRuntime(c.Request.Context(), p.config)
	return c.JSON(http.StatusOK, snapshot)
}
