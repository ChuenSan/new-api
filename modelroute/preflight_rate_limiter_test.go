package modelroute

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestPreflightRateLimiter_Unlimited(t *testing.T) {
	limiter := &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	ctx := context.Background()
	// T=0, N=0 -> unlimited
	for i := 0; i < 10; i++ {
		if err := limiter.Acquire(ctx, 1, "gpt-4o"); err != nil {
			t.Fatalf("expected nil error on unlimited, got: %v", err)
		}
	}
}

func TestPreflightRateLimiter_UnderLimit(t *testing.T) {
	limiter := &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	w := 1
	n := 5
	limiter.UpdateOverride(1, "gpt-4o", &w, &n)

	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := limiter.Acquire(ctx, 1, "gpt-4o"); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected 5 acquires under limit to be immediate, took %v", elapsed)
	}
}

func TestPreflightRateLimiter_PacingWait(t *testing.T) {
	limiter := &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	w := 1 // 1 second window
	n := 2 // max 2 requests
	limiter.UpdateOverride(2, "gpt-4o", &w, &n)

	ctx := context.Background()
	// First 2 should be immediate
	if err := limiter.Acquire(ctx, 2, "gpt-4o"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := limiter.Acquire(ctx, 2, "gpt-4o"); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	// 3rd should wait for window release
	start := time.Now()
	if err := limiter.Acquire(ctx, 2, "gpt-4o"); err != nil {
		t.Fatalf("third acquire failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected pacing wait of ~1s, took %v", elapsed)
	}
}

func TestPreflightRateLimiter_ContextCancel(t *testing.T) {
	limiter := &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	w := 5 // 5 seconds window
	n := 1 // max 1 request
	limiter.UpdateOverride(3, "gpt-4o", &w, &n)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// First acquire consumes the quota
	if err := limiter.Acquire(context.Background(), 3, "gpt-4o"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should block and get cancelled by context
	err := limiter.Acquire(ctx, 3, "gpt-4o")
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestPreflightRateLimiter_Concurrent(t *testing.T) {
	limiter := &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	w := 1
	n := 10
	limiter.UpdateOverride(4, "claude-3-5-sonnet", &w, &n)

	var wg sync.WaitGroup
	ctx := context.Background()
	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Acquire(ctx, 4, "claude-3-5-sonnet"); err != nil {
				t.Errorf("concurrent acquire failed: %v", err)
			}
		}()
	}

	wg.Wait()
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("expected 10 concurrent requests to pass within 1s window, took %v", elapsed)
	}
}

func TestGetPreflightRateLimit_Priority(t *testing.T) {
	// 1. Per-route override wins
	w := 10
	n := 30
	m := &model.ChannelModelMetrics{
		RateLimitWindowSeconds: &w,
		RateLimitMaxRequests:   &n,
	}
	effW, effN := GetPreflightRateLimit(m)
	if effW != 10 || effN != 30 {
		t.Fatalf("expected 10, 30 from override, got %d, %d", effW, effN)
	}

	// 2. Nil override falls back to global setting
	mNil := &model.ChannelModelMetrics{}
	effW2, effN2 := GetPreflightRateLimit(mNil)
	if effW2 != operation_setting.GetRateLimitWindowSeconds() || effN2 != operation_setting.GetRateLimitMaxRequests() {
		t.Fatalf("expected fallback to global, got %d, %d", effW2, effN2)
	}
}
