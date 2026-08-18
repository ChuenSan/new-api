package modelroute

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// GetRateLimitCircuitBreakerThreshold returns the route override when it is
// valid, otherwise the process-wide setting.
func GetRateLimitCircuitBreakerThreshold(m *model.ChannelModelMetrics) int {
	if m != nil && m.RateLimitCircuitBreakerThreshold != nil {
		threshold := *m.RateLimitCircuitBreakerThreshold
		if threshold >= operation_setting.MinRateLimitCircuitBreakerThreshold &&
			threshold <= operation_setting.MaxRateLimitCircuitBreakerThreshold {
			return threshold
		}
	}
	return operation_setting.GetRateLimitCircuitBreakerThreshold()
}

// UpdateRateLimitCircuitBreakerThreshold persists a per-route override and
// updates the hot runtime copy while holding the same route-key lock used by
// state transitions.
func UpdateRateLimitCircuitBreakerThreshold(
	channelID int64,
	effectiveModel string,
	threshold *int,
) (*model.ChannelModelMetrics, error) {
	mk := MakeMetricsKey(channelID, effectiveModel)
	lock := metricsLockFor(mk)
	lock.Lock()
	defer lock.Unlock()

	updated, err := model.UpdateChannelModelMetricsRateLimitThreshold(channelID, effectiveModel, threshold)
	if err != nil {
		return nil, err
	}

	if current := GlobalMetricsRuntime.Get(mk); current != nil {
		current.RateLimitCircuitBreakerThreshold = cloneThreshold(threshold)
		current.UpdatedAt = updated.UpdatedAt
		GlobalMetricsRuntime.Put(current)
		return current, nil
	}
	updated.RateLimitCircuitBreakerThreshold = cloneThreshold(threshold)
	GlobalMetricsRuntime.Put(updated)
	return updated, nil
}

func cloneThreshold(threshold *int) *int {
	if threshold == nil {
		return nil
	}
	value := *threshold
	return &value
}
