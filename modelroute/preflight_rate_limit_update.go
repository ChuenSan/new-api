package modelroute

import "github.com/QuantumNous/new-api/model"

func UpdatePreflightRateLimit(
	channelID int64,
	effectiveModel string,
	windowSeconds *int,
	maxRequests *int,
) (*model.ChannelModelMetrics, error) {
	mk := MakeMetricsKey(channelID, effectiveModel)
	lock := metricsLockFor(mk)
	lock.Lock()
	defer lock.Unlock()

	updated, err := model.UpdateChannelModelMetricsRateLimit(channelID, effectiveModel, windowSeconds, maxRequests)
	if err != nil {
		return nil, err
	}

	if current := GlobalMetricsRuntime.Get(mk); current != nil {
		current.RateLimitWindowSeconds = cloneIntPtr(windowSeconds)
		current.RateLimitMaxRequests = cloneIntPtr(maxRequests)
		current.UpdatedAt = updated.UpdatedAt
		GlobalMetricsRuntime.Put(current)
	} else {
		updated.RateLimitWindowSeconds = cloneIntPtr(windowSeconds)
		updated.RateLimitMaxRequests = cloneIntPtr(maxRequests)
		GlobalMetricsRuntime.Put(updated)
	}

	if GlobalPreflightRateLimiter != nil {
		GlobalPreflightRateLimiter.UpdateOverride(channelID, effectiveModel, windowSeconds, maxRequests)
	}

	return updated, nil
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
