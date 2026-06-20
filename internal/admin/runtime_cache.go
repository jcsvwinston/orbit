package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const adminRedisDialTimeout = 500 * time.Millisecond

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
