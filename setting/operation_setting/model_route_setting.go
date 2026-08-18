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
