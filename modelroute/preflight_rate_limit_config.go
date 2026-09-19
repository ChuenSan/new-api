package modelroute

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// GetPreflightRateLimit returns the effective (windowSeconds, maxRequests).
// Priority: per-route override > global default > unlimited (0,0).
func GetPreflightRateLimit(m *model.ChannelModelMetrics) (window int, max int) {
	if m != nil && m.RateLimitWindowSeconds != nil && m.RateLimitMaxRequests != nil {
		w, n := *m.RateLimitWindowSeconds, *m.RateLimitMaxRequests
		if w > 0 && n > 0 {
			return w, n
		}
	}
	return operation_setting.GetRateLimitWindowSeconds(), operation_setting.GetRateLimitMaxRequests()
}
