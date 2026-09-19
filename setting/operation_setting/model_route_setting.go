package operation_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultRateLimitCircuitBreakerThreshold = 3
	MinRateLimitCircuitBreakerThreshold     = 3
	MaxRateLimitCircuitBreakerThreshold     = 999
)

// ModelRouteSetting contains process-wide model route reliability settings.
type ModelRouteSetting struct {
	RateLimitCircuitBreakerThreshold int `json:"rate_limit_circuit_breaker_threshold"`
	RateLimitWindowSeconds           int `json:"rate_limit_window_seconds"`
	RateLimitMaxRequests             int `json:"rate_limit_max_requests"`
}

var modelRouteSetting = ModelRouteSetting{
	RateLimitCircuitBreakerThreshold: DefaultRateLimitCircuitBreakerThreshold,
}

func init() {
	config.GlobalConfig.Register("model_route_setting", &modelRouteSetting)
}

func GetModelRouteSetting() *ModelRouteSetting {
	return &modelRouteSetting
}

// GetRateLimitCircuitBreakerThreshold returns a safe value even if a legacy or
// manually edited database option contains an invalid value.
func GetRateLimitCircuitBreakerThreshold() int {
	threshold := modelRouteSetting.RateLimitCircuitBreakerThreshold
	if threshold < MinRateLimitCircuitBreakerThreshold || threshold > MaxRateLimitCircuitBreakerThreshold {
		return DefaultRateLimitCircuitBreakerThreshold
	}
	return threshold
}

func GetRateLimitWindowSeconds() int {
	v := modelRouteSetting.RateLimitWindowSeconds
	if v < 0 {
		return 0
	}
	return v
}

func GetRateLimitMaxRequests() int {
	v := modelRouteSetting.RateLimitMaxRequests
	if v < 0 {
		return 0
	}
	return v
}

func ValidateRateLimitCircuitBreakerThreshold(value string) (int, error) {
	threshold, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || threshold < MinRateLimitCircuitBreakerThreshold || threshold > MaxRateLimitCircuitBreakerThreshold {
		return 0, fmt.Errorf(
			"rate-limit circuit breaker threshold must be an integer from %d to %d",
			MinRateLimitCircuitBreakerThreshold,
			MaxRateLimitCircuitBreakerThreshold,
		)
	}
	return threshold, nil
}

func ValidateRateLimitWindowSeconds(value string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || v < 0 || v > 86400 {
		return 0, fmt.Errorf("rate-limit window seconds must be an integer from 0 to 86400")
	}
	return v, nil
}

func ValidateRateLimitMaxRequests(value string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || v < 0 || v > 1000000 {
		return 0, fmt.Errorf("rate-limit max requests must be an integer from 0 to 1000000")
	}
	return v, nil
}
