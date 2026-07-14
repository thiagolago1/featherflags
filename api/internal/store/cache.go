package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache TTLs. Rule sets are also invalidated explicitly on admin writes, so
// the TTL is only a safety net (e.g. writes from another instance).
const (
	rulesTTL  = 60 * time.Second
	apiKeyTTL = 5 * time.Minute // key→project/env mapping is immutable
)

// EnableCache attaches an optional Redis cache. Any Redis failure degrades to
// a plain DB read — the flags service must never go down because of its cache.
func (s *Store) EnableCache(ctx context.Context, redisURL string) error {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return err
	}
	s.redis = client
	return nil
}

func rulesCacheKey(projectID, env string) string { return "ff:rules:" + projectID + ":" + env }
func apiKeyCacheKey(apiKey string) string        { return "ff:key:" + apiKey }

type keyInfo struct {
	ProjectID string `json:"p"`
	Env       string `json:"e"`
}

func (s *Store) cachedKeyInfo(ctx context.Context, apiKey string) (keyInfo, bool) {
	if s.redis == nil {
		return keyInfo{}, false
	}
	raw, err := s.redis.Get(ctx, apiKeyCacheKey(apiKey)).Bytes()
	if err != nil {
		return keyInfo{}, false
	}
	var ki keyInfo
	if json.Unmarshal(raw, &ki) != nil {
		return keyInfo{}, false
	}
	return ki, true
}

func (s *Store) cachedRules(ctx context.Context, projectID, env string) ([]EvalRule, bool) {
	if s.redis == nil {
		return nil, false
	}
	raw, err := s.redis.Get(ctx, rulesCacheKey(projectID, env)).Bytes()
	if err != nil {
		return nil, false
	}
	var rules []EvalRule
	if json.Unmarshal(raw, &rules) != nil {
		return nil, false
	}
	return rules, true
}

func (s *Store) storeCache(ctx context.Context, key string, v any, ttl time.Duration) {
	if s.redis == nil {
		return
	}
	if raw, err := json.Marshal(v); err == nil {
		s.redis.Set(ctx, key, raw, ttl)
	}
}

// invalidateProject drops the cached rule sets for every environment of a
// project. Called after any admin write that can change evaluation results.
func (s *Store) invalidateProject(ctx context.Context, projectID string) {
	if s.redis == nil {
		return
	}
	keys := make([]string, 0, len(Environments))
	for _, env := range Environments {
		keys = append(keys, rulesCacheKey(projectID, env))
	}
	s.redis.Del(ctx, keys...)
}

// projectIDForFlag resolves the owning project of a flag (for invalidation).
func (s *Store) projectIDForFlag(ctx context.Context, flagID string) (string, error) {
	var projectID string
	err := s.pool.QueryRow(ctx, `SELECT project_id FROM flags WHERE id = $1`, flagID).Scan(&projectID)
	return projectID, err
}
