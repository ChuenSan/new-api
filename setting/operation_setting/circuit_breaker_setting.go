package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// CircuitBreakerSetting holds global defaults for the in-process circuit breaker.
// A channel may override these via dto.ChannelOtherSettings. The breaker is only
// active when auto_ban is disabled (auto_ban=0); auto_ban=1 channels keep using
// the existing auto-ban path and never touch the breaker.
type CircuitBreakerSetting struct {
	// FailureThreshold is the number of consecutive non-2xx failures required to
	// trip a channel:model from CLOSED to OPEN.
	FailureThreshold int `json:"failure_threshold"`
	// CooldownSeconds is how long an OPEN entry stays open before transitioning
	// to HALF_OPEN for a single probe request.
	CooldownSeconds int `json:"cooldown_seconds"`
}

var circuitBreakerSetting = CircuitBreakerSetting{
	FailureThreshold: 3,
	CooldownSeconds:  60,
}

func init() {
	config.GlobalConfig.Register("circuit_breaker_setting", &circuitBreakerSetting)
}

func GetCircuitBreakerSetting() *CircuitBreakerSetting {
	return &circuitBreakerSetting
}
