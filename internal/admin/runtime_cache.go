package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const adminRedisDialTimeout = 500 * time.Millisecond

// Cache is the cache an application declares to the panel, so the cache view
// reports — and the flush button empties — the cache the application
// actually has (OR-49).
//
// It exists because there is nothing for the panel to discover. Nucleus
// ships pkg/cache, but nothing in the framework wires one into an
// application: a cache is built, owned and handed around by the application
// itself, so the application is the only thing that can say which one the
// panel is looking at. Before this contract the view knew a single cache —
// a Redis URL passed in configuration — and answered every other
// application "redis url is not configured", including the many that have
// no Redis at all and never asked for one.
//
// The panel neither reads nor writes entries: it counts them and empties
// them, which is the whole of what an operator does to a cache from a
// screen. An implementation that cannot count says so through known=false
// rather than guessing a number.
type Cache interface {
	// CacheName labels the cache in the view ("redis", "in-process", the
	// name of a shared cluster). It is shown to an operator, so it should
	// identify WHICH cache, not restate its type.
	CacheName() string
	// CacheEntries returns how many entries the cache holds. known=false
	// means the backend cannot answer; the view then shows the cache
	// without a count instead of showing zero, which would read as empty.
	CacheEntries(ctx context.Context) (count int64, known bool, err error)
	// FlushCache empties the cache and returns how many entries went, or -1
	// when the backend cannot say. It is the destructive half of the
	// contract: the panel audits every call.
	FlushCache(ctx context.Context) (removed int64, err error)
}

// cacheRuntimeSnapshot is what the cache view answers, for any of the three
// postures: a declared cache, the legacy Redis URL, or no cache at all.
type cacheRuntimeSnapshot struct {
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // declared | redis | none
	Name    string `json:"name,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// CanFlush tells the UI whether to offer the button at all. A screen
	// that offers an action nothing can perform teaches operators to
	// ignore errors.
	CanFlush     bool  `json:"can_flush"`
	Entries      int64 `json:"entries,omitempty"`
	EntriesKnown bool  `json:"entries_known"`
	// RedisURL and LatencyMS are only meaningful for the redis posture and
	// stay out of the payload otherwise.
	RedisURL  string `json:"redis_url,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// inspectCacheRuntime answers for whichever cache the application has: the
// one it declared first, the configured Redis second, and neither third.
// A declared cache wins over a Redis URL because it is the application's own
// statement about what its cache is.
func inspectCacheRuntime(ctx context.Context, declared Cache, redisURL string) cacheRuntimeSnapshot {
	if declared != nil {
		snapshot := cacheRuntimeSnapshot{
			Enabled:  true,
			Kind:     "declared",
			Name:     declared.CacheName(),
			Status:   "healthy",
			Message:  "cache declared by the application",
			CanFlush: true,
		}
		count, known, err := declared.CacheEntries(ctx)
		if err != nil {
			snapshot.Status = "degraded"
			snapshot.Message = fmt.Sprintf("cache declared but failed to inspect: %v", err)
			return snapshot
		}
		snapshot.Entries, snapshot.EntriesKnown = count, known
		return snapshot
	}

	if strings.TrimSpace(redisURL) == "" {
		// The honest answer for the default application: there is no cache
		// here, which is not the same as a cache that is unreachable.
		return cacheRuntimeSnapshot{
			Enabled:  false,
			Kind:     "none",
			Status:   "disabled",
			Message:  "this application has not declared a cache",
			CanFlush: false,
		}
	}

	redis := inspectRedisRuntime(ctx, redisURL)
	return cacheRuntimeSnapshot{
		Enabled:      redis.Enabled,
		Kind:         "redis",
		Name:         "redis",
		Status:       redis.Status,
		Message:      redis.Message,
		CanFlush:     redis.Status == "healthy" || redis.Status == "degraded",
		Entries:      redis.KeyCount,
		EntriesKnown: redis.Status == "healthy",
		RedisURL:     redis.RedisURL,
		LatencyMS:    redis.LatencyMS,
	}
}

// flushCacheResult is what the flush answers, whichever cache it emptied.
type flushCacheResult struct {
	Flushed bool   `json:"flushed"`
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
	// Removed is how many entries went; RemovedKnown is false when the
	// backend cannot say, so nothing reads an unknown count as zero.
	Removed      int64 `json:"removed,omitempty"`
	RemovedKnown bool  `json:"removed_known"`
	// KeyCountBefore/After are what this endpoint answered when a cache
	// could only be Redis. They are carried alongside the new fields
	// rather than replaced (QADR-0010: the new shape ships beside the old
	// one), and are only meaningful for the redis posture.
	KeyCountBefore int64  `json:"key_count_before,omitempty"`
	KeyCountAfter  int64  `json:"key_count_after,omitempty"`
	RedisURL       string `json:"redis_url,omitempty"`
	Status         string `json:"status,omitempty"`
	LatencyMS      int64  `json:"latency_ms,omitempty"`
}

type redisRuntimeSnapshot struct {
	Enabled   bool   `json:"enabled"`
	RedisURL  string `json:"redis_url,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	KeyCount  int64  `json:"key_count,omitempty"`
}

type redisFlushResult struct {
	RedisURL       string `json:"redis_url,omitempty"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	LatencyMS      int64  `json:"latency_ms,omitempty"`
	KeyCountBefore int64  `json:"key_count_before,omitempty"`
	KeyCountAfter  int64  `json:"key_count_after,omitempty"`
}

func inspectRedisRuntime(ctx context.Context, redisURL string) redisRuntimeSnapshot {
	redisURL = strings.TrimSpace(redisURL)
	if redisURL == "" {
		return redisRuntimeSnapshot{
			Enabled: false,
			Status:  "disabled",
			Message: "redis url is not configured",
		}
	}

	client, err := newAdminRedisClient(redisURL)
	if err != nil {
		return redisRuntimeSnapshot{
			Enabled:  true,
			RedisURL: redisURL,
			Status:   "unhealthy",
			Message:  err.Error(),
		}
	}
	defer client.Close()

	start := time.Now()
	if err := client.Ping(ctx).Err(); err != nil {
		return redisRuntimeSnapshot{
			Enabled:   true,
			RedisURL:  redisURL,
			Status:    "unhealthy",
			Message:   err.Error(),
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}

	keyCount, err := client.DBSize(ctx).Result()
	if err != nil {
		return redisRuntimeSnapshot{
			Enabled:   true,
			RedisURL:  redisURL,
			Status:    "degraded",
			Message:   fmt.Sprintf("connected but failed to inspect cache size: %v", err),
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}

	return redisRuntimeSnapshot{
		Enabled:   true,
		RedisURL:  redisURL,
		Status:    "healthy",
		Message:   "connected",
		LatencyMS: time.Since(start).Milliseconds(),
		KeyCount:  keyCount,
	}
}

func flushRedisRuntime(ctx context.Context, redisURL string) (redisFlushResult, error) {
	redisURL = strings.TrimSpace(redisURL)
	if redisURL == "" {
		return redisFlushResult{}, fmt.Errorf("redis url is not configured")
	}

	client, err := newAdminRedisClient(redisURL)
	if err != nil {
		return redisFlushResult{}, err
	}
	defer client.Close()

	start := time.Now()
	before, err := client.DBSize(ctx).Result()
	if err != nil {
		return redisFlushResult{}, err
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		return redisFlushResult{}, err
	}
	after, err := client.DBSize(ctx).Result()
	if err != nil {
		return redisFlushResult{}, err
	}

	return redisFlushResult{
		RedisURL:       redisURL,
		Status:         "healthy",
		Message:        "cache flushed",
		LatencyMS:      time.Since(start).Milliseconds(),
		KeyCountBefore: before,
		KeyCountAfter:  after,
	}, nil
}

func newAdminRedisClient(redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	options.DialTimeout = adminRedisDialTimeout
	options.ReadTimeout = adminRedisDialTimeout
	options.WriteTimeout = adminRedisDialTimeout
	return redis.NewClient(options), nil
}

// flushCacheRuntime empties whichever cache the application has, and refuses
// in the one case where there is nothing to empty.
//
// The refusal is the point of OR-49: the old flush answered "redis url is not
// configured" to an application that has no Redis and never asked for one,
// which reads as a misconfiguration the operator should go and fix. It is
// not one — there is simply no cache here — and the view now says that
// instead, with the button withheld (CanFlush) rather than offered and
// refused.
func flushCacheRuntime(ctx context.Context, declared Cache, redisURL string) (flushCacheResult, error) {
	if declared != nil {
		removed, err := declared.FlushCache(ctx)
		if err != nil {
			return flushCacheResult{}, err
		}
		return flushCacheResult{
			Flushed:      true,
			Kind:         "declared",
			Name:         declared.CacheName(),
			Message:      "cache flushed",
			Removed:      max64(removed, 0),
			RemovedKnown: removed >= 0,
		}, nil
	}

	if strings.TrimSpace(redisURL) == "" {
		return flushCacheResult{}, errNoCacheDeclared
	}

	result, err := flushRedisRuntime(ctx, redisURL)
	if err != nil {
		return flushCacheResult{}, err
	}
	return flushCacheResult{
		Flushed:        true,
		Kind:           "redis",
		Name:           "redis",
		Message:        result.Message,
		Removed:        result.KeyCountBefore - result.KeyCountAfter,
		RemovedKnown:   true,
		KeyCountBefore: result.KeyCountBefore,
		KeyCountAfter:  result.KeyCountAfter,
		RedisURL:       result.RedisURL,
		Status:         result.Status,
		LatencyMS:      result.LatencyMS,
	}, nil
}

// errNoCacheDeclared is the refusal an application with no cache gets. It
// names the absence, not a missing setting.
var errNoCacheDeclared = errors.New("this application has not declared a cache, so there is nothing to flush")

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
