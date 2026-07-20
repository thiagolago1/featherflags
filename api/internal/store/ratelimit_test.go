package store

import (
	"context"
	"testing"
	"time"
)

func TestAllowLocalFallback(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if !s.Allow(ctx, "k", 3, time.Minute) {
			t.Fatalf("request %d should be allowed within limit", i)
		}
	}
	if s.Allow(ctx, "k", 3, time.Minute) {
		t.Fatal("4th request should be rejected once limit is exhausted")
	}

	// A different key has its own budget.
	if !s.Allow(ctx, "other", 3, time.Minute) {
		t.Fatal("distinct key should not share the exhausted window")
	}
}

func TestAllowLocalWindowResets(t *testing.T) {
	s := &Store{}
	ctx := context.Background()

	if !s.Allow(ctx, "k", 1, 10*time.Millisecond) {
		t.Fatal("first request should be allowed")
	}
	if s.Allow(ctx, "k", 1, 10*time.Millisecond) {
		t.Fatal("second request should be rejected within the window")
	}
	time.Sleep(20 * time.Millisecond)
	if !s.Allow(ctx, "k", 1, 10*time.Millisecond) {
		t.Fatal("request after window reset should be allowed again")
	}
}
