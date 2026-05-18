// Package ratelimit is the Redis-backed store backing
// app/ratelimit.Store. The Lua refill+consume script collapses the
// read-modify-write into a single round-trip so concurrent requests from
// the same key don't double-spend the bucket.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	appratelimit "github.com/dtszwai/media-processing-service/backend/internal/app/ratelimit"
	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
	script *redis.Script
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{
		client: client,
		script: redis.NewScript(luaTokenBucket),
	}
}

func (s *RedisStore) Allow(ctx context.Context, keys []string, cfg appratelimit.Bucket) (appratelimit.Decision, error) {
	if len(keys) == 0 {
		return appratelimit.Decision{}, errors.New("ratelimit: no bucket keys")
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1
	}
	if cfg.RefillPerSecond <= 0 {
		cfg.RefillPerSecond = 1
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 2 * time.Minute
	}
	raw, err := s.script.Run(ctx, s.client, keys,
		cfg.Capacity,
		strconv.FormatFloat(cfg.RefillPerSecond, 'f', -1, 64),
		cfg.Now.UnixMilli(),
		cfg.TTL.Milliseconds(),
	).Result()
	if err != nil {
		return appratelimit.Decision{}, fmt.Errorf("ratelimit: redis lua: %w", err)
	}
	values, ok := raw.([]any)
	if !ok || len(values) < 3 {
		return appratelimit.Decision{}, fmt.Errorf("ratelimit: unexpected redis response %T", raw)
	}
	allowed, err := asInt64(values[0])
	if err != nil {
		return appratelimit.Decision{}, err
	}
	remaining, err := asInt64(values[1])
	if err != nil {
		return appratelimit.Decision{}, err
	}
	resetMS, err := asInt64(values[2])
	if err != nil {
		return appratelimit.Decision{}, err
	}
	reset := time.Duration(resetMS) * time.Millisecond
	decision := appratelimit.Decision{
		Allowed:    allowed == 1,
		Limit:      cfg.Capacity,
		Remaining:  int(remaining),
		ResetAfter: reset,
	}
	if !decision.Allowed {
		if reset <= 0 {
			reset = time.Second
		}
		decision.RetryAfter = reset
	}
	return decision, nil
}

func asInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("ratelimit: parse int %q: %w", x, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("ratelimit: unexpected int type %T", v)
	}
}

const luaTokenBucket = `
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local ttl_ms = tonumber(ARGV[4])
local min_remaining = capacity
local max_reset_ms = 0
local states = {}

for i, key in ipairs(KEYS) do
  local row = redis.call("HMGET", key, "tokens", "refreshed_at_ms")
  local tokens = tonumber(row[1])
  local refreshed = tonumber(row[2])
  if tokens == nil then
    tokens = capacity
  end
  if refreshed == nil then
    refreshed = now_ms
  end
  local elapsed = math.max(0, now_ms - refreshed) / 1000
  tokens = math.min(capacity, tokens + elapsed * refill)
  states[i] = { key = key, tokens = tokens }
  min_remaining = math.min(min_remaining, math.floor(tokens))
  if tokens < 1 then
    local reset_ms = math.ceil((1 - tokens) / refill * 1000)
    max_reset_ms = math.max(max_reset_ms, reset_ms)
  end
end

if max_reset_ms > 0 then
  return {0, math.max(0, min_remaining), max_reset_ms}
end

min_remaining = capacity
for _, state in ipairs(states) do
  local tokens = state.tokens - 1
  min_remaining = math.min(min_remaining, math.floor(tokens))
  redis.call("HSET", state.key, "tokens", tokens, "refreshed_at_ms", now_ms, "capacity", capacity, "refill_per_sec", refill)
  redis.call("PEXPIRE", state.key, ttl_ms)
end

local reset_ms = math.ceil((capacity - min_remaining) / refill * 1000)
return {1, math.max(0, min_remaining), reset_ms}
`
