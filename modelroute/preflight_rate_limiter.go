package modelroute

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var GlobalPreflightRateLimiter *PreflightRateLimiter

type PreflightRateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*slidingWindowBucket
}

type slidingWindowBucket struct {
	mu            sync.Mutex
	timestamps    []time.Time
	windowSeconds *int
	maxRequests   *int
}

func bucketKey(channelID int64, effectiveModel string) string {
	return strconv.FormatInt(channelID, 10) + ":" + effectiveModel
}

func (l *PreflightRateLimiter) getBucket(key string) *slidingWindowBucket {
	l.mu.RLock()
	b, ok := l.buckets[key]
	l.mu.RUnlock()
	if ok {
		return b
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[key]; ok {
		return b
	}
	b = &slidingWindowBucket{
		timestamps: make([]time.Time, 0),
	}
	l.buckets[key] = b
	return b
}

func (l *PreflightRateLimiter) UpdateOverride(channelID int64, effectiveModel string, windowSeconds, maxRequests *int) {
	key := bucketKey(channelID, effectiveModel)
	b := l.getBucket(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.windowSeconds = cloneIntPtr(windowSeconds)
	b.maxRequests = cloneIntPtr(maxRequests)
}

func (l *PreflightRateLimiter) Acquire(ctx context.Context, channelID int64, effectiveModel string) error {
	key := bucketKey(channelID, effectiveModel)
	b := l.getBucket(key)

	for {
		b.mu.Lock()
		var w, m int
		if b.windowSeconds != nil && b.maxRequests != nil && *b.windowSeconds > 0 && *b.maxRequests > 0 {
			w, m = *b.windowSeconds, *b.maxRequests
		} else {
			w = operation_setting.GetRateLimitWindowSeconds()
			m = operation_setting.GetRateLimitMaxRequests()
		}

		if w <= 0 || m <= 0 {
			b.mu.Unlock()
			return nil
		}

		now := time.Now()
		windowDuration := time.Duration(w) * time.Second
		cutoff := now.Add(-windowDuration)

		var i int
		for i = 0; i < len(b.timestamps); i++ {
			if !b.timestamps[i].Before(cutoff) {
				break
			}
		}
		if i > 0 {
			b.timestamps = b.timestamps[i:]
		}

		if len(b.timestamps) < m {
			b.timestamps = append(b.timestamps, now)
			b.mu.Unlock()
			return nil
		}

		wait := b.timestamps[0].Add(windowDuration).Sub(now)
		b.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func InitPreflightRateLimiter() {
	GlobalPreflightRateLimiter = &PreflightRateLimiter{
		buckets: make(map[string]*slidingWindowBucket),
	}

	GlobalMetricsRuntime.mu.RLock()
	defer GlobalMetricsRuntime.mu.RUnlock()
	for _, m := range GlobalMetricsRuntime.data {
		if m != nil && m.RateLimitWindowSeconds != nil && m.RateLimitMaxRequests != nil {
			GlobalPreflightRateLimiter.UpdateOverride(m.ChannelID, m.EffectiveModel, m.RateLimitWindowSeconds, m.RateLimitMaxRequests)
		}
	}
}
