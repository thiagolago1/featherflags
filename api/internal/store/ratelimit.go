package store

import (
	"context"
	"time"
)

// Allow implements a fixed-window rate limiter keyed by an arbitrary string
// (e.g. "evaluate:1.2.3.4"). It is backed by Redis when available (so limits
// are shared across replicas); otherwise it falls back to an in-memory
// per-process window, which still protects a single instance.
func (s *Store) Allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	if s.redis != nil {
		count, err := s.redis.Incr(ctx, "ff:rl:"+key).Result()
		if err != nil {
			// Redis hiccup: fail open rather than taking the API down.
			return true
		}
		if count == 1 {
			s.redis.Expire(ctx, "ff:rl:"+key, window)
		}
		return count <= int64(limit)
	}
	return s.allowLocal(key, limit, window)
}

type localWindow struct {
	count int
	reset time.Time
}

func (s *Store) allowLocal(key string, limit int, window time.Duration) bool {
	s.localMu.Lock()
	defer s.localMu.Unlock()
	if s.localWindows == nil {
		s.localWindows = make(map[string]*localWindow)
	}
	now := time.Now()
	w, ok := s.localWindows[key]
	if !ok || now.After(w.reset) {
		w = &localWindow{count: 0, reset: now.Add(window)}
		s.localWindows[key] = w
	}
	w.count++
	return w.count <= limit
}
