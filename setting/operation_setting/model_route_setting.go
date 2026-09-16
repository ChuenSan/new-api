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

	DefaultRetryDelayMin = 1
	DefaultRetryDelayMax = 3
	MinRetryDelay        = 0
	MaxRetryDelay        = 60
)

// ModelRouteSetting contains process-wide model route reliability settings.
type ModelRouteSetting struct {
	RateLimitCircuitBreakerThreshold int `json:"rate_limit_circuit_breaker_threshold"`
	RetryDelayMin                    int `json:"retry_delay_min"`
	RetryDelayMax                    int `json:"retry_delay_max"`
}

var modelRouteSetting = ModelRouteSetting{
	RateLimitCircuitBreakerThreshold: DefaultRateLimitCircuitBreakerThreshold,
	RetryDelayMin:                    DefaultRetryDelayMin,
	RetryDelayMax:                    DefaultRetryDelayMax,
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

// GetRetryDelayRange returns safe (minSeconds, maxSeconds) for retry backoff.
func GetRetryDelayRange() (int, int) {
	minDelay := modelRouteSetting.RetryDelayMin
	maxDelay := modelRouteSetting.RetryDelayMax
	if minDelay < MinRetryDelay || minDelay > MaxRetryDelay {
		minDelay = DefaultRetryDelayMin
	}
	if maxDelay < MinRetryDelay || maxDelay > MaxRetryDelay {
		maxDelay = DefaultRetryDelayMax
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return minDelay, maxDelay
}

func ValidateRetryDelay(value string) (int, error) {
	delay, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || delay < MinRetryDelay || delay > MaxRetryDelay {
		return 0, fmt.Errorf(
			"retry delay must be an integer from %d to %d",
			MinRetryDelay,
			MaxRetryDelay,
		)
	}
	return delay, nil
}

