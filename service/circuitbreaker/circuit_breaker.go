// Package circuitbreaker implements a lightweight, in-process circuit breaker
// keyed by channel_id:model_name. It is intentionally process-local: no state
// is written to the database or Redis, and there is no multi-instance sync.
//
// The breaker is only active for channels with auto_ban disabled (auto_ban=0).
// Channels with auto_ban=1 keep using the existing auto-ban path and never
// interact with this package.
package circuitbreaker

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// ProbeTimeout bounds how long a granted HALF_OPEN probe may stay in flight
// before it is considered leaked and recycled for another probe.
const ProbeTimeout = 30 * time.Second

type entry struct {
	state         State
	failCount     int
	openedAt      time.Time
	cooldown      time.Duration
	probeInFlight bool
	probeSince    time.Time
}

var (
	mu    sync.RWMutex
	state = make(map[string]*entry)
	// now is indirected so tests can advance time deterministically without sleeps.
	now = time.Now
)

func key(channelId int, modelName string) string {
	return fmt.Sprintf("%d:%s", channelId, modelName)
}

// IsOpen reports whether the channel:model is currently OPEN (blocking all
// traffic). If the OPEN cooldown has elapsed, it lazily transitions the entry
// to HALF_OPEN and returns false.
func IsOpen(channelId int, modelName string) bool {
	mu.Lock()
	defer mu.Unlock()
	e, ok := state[key(channelId, modelName)]
	if !ok || e.state != StateOpen {
		return false
	}
	if now().Sub(e.openedAt) >= e.cooldown {
		e.state = StateHalfOpen
		e.probeInFlight = false
		return false
	}
	return true
}

// IsHalfOpen reports whether the channel:model is currently in HALF_OPEN
// (probing). It is a pure state query and does not mutate.
func IsHalfOpen(channelId int, modelName string) bool {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := state[key(channelId, modelName)]
	return ok && e.state == StateHalfOpen
}

// AllowProbe attempts to claim the single HALF_OPEN probe slot. Returns true
// if this caller may send the probe request. A leaked probe (in flight longer
// than ProbeTimeout) is recycled so the breaker cannot get stuck.
func AllowProbe(channelId int, modelName string) bool {
	mu.Lock()
	defer mu.Unlock()
	e, ok := state[key(channelId, modelName)]
	if !ok || e.state != StateHalfOpen {
		return false
	}
	if e.probeInFlight && now().Sub(e.probeSince) < ProbeTimeout {
		return false
	}
	e.probeInFlight = true
	e.probeSince = now()
	return true
}

// RecordSuccess records a 2xx success. Any success resets the failure counter
// to zero; from HALF_OPEN it closes the circuit, from CLOSED it clears partial
// failures.
func RecordSuccess(channelId int, modelName string) {
	mu.Lock()
	defer mu.Unlock()
	k := key(channelId, modelName)
	e, ok := state[k]
	if !ok {
		return
	}
	if e.state == StateHalfOpen {
		common.SysLog(fmt.Sprintf("circuit breaker recovered: channel #%d model %s HALF_OPEN -> CLOSED", channelId, modelName))
	}
	// A success always means healthy: drop the entry entirely.
	delete(state, k)
}

// RecordFailure records a non-2xx failure and drives the state machine. A 2xx
// status (treated as success) resets the entry. settings provides the per-channel
// override for the failure threshold; pass nil to use the global default.
func RecordFailure(channelId int, modelName string, statusCode int, settings *dto.ChannelOtherSettings) {
	if statusCode >= 200 && statusCode < 300 {
		RecordSuccess(channelId, modelName)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	k := key(channelId, modelName)
	e, ok := state[k]
	if !ok {
		e = &entry{state: StateClosed}
		state[k] = e
	}
	switch e.state {
	case StateClosed:
		e.failCount++
		if e.failCount >= GetFailureThreshold(settings) {
			e.state = StateOpen
			e.openedAt = now()
			e.cooldown = time.Duration(GetCooldownSeconds(settings)) * time.Second
			e.failCount = 0
			common.SysLog(fmt.Sprintf("circuit breaker tripped: channel #%d model %s CLOSED -> OPEN (cooldown %s)", channelId, modelName, e.cooldown))
		}
	case StateHalfOpen:
		// Probe failed: re-open and restart the cooldown window.
		e.state = StateOpen
		e.openedAt = now()
		e.cooldown = time.Duration(GetCooldownSeconds(settings)) * time.Second
		e.probeInFlight = false
		e.failCount = 0
		common.SysLog(fmt.Sprintf("circuit breaker probe failed: channel #%d model %s HALF_OPEN -> OPEN (cooldown %s)", channelId, modelName, e.cooldown))
	case StateOpen:
		// OPEN channels are filtered out of selection, so failures here are
		// leftover from in-flight requests started before the trip. Ignore.
	}
}

// GetFailureThreshold resolves the per-channel override, falling back to the
// global default. Non-positive overrides are rejected.
func GetFailureThreshold(settings *dto.ChannelOtherSettings) int {
	if settings != nil && settings.CircuitBreakerFailureThreshold != nil && *settings.CircuitBreakerFailureThreshold > 0 {
		return *settings.CircuitBreakerFailureThreshold
	}
	return operation_setting.GetCircuitBreakerSetting().FailureThreshold
}

// GetCooldownSeconds resolves the per-channel override, falling back to the
// global default. Non-positive overrides are rejected.
func GetCooldownSeconds(settings *dto.ChannelOtherSettings) int {
	if settings != nil && settings.CircuitBreakerCooldownSeconds != nil && *settings.CircuitBreakerCooldownSeconds > 0 {
		return *settings.CircuitBreakerCooldownSeconds
	}
	return operation_setting.GetCircuitBreakerSetting().CooldownSeconds
}

// reset clears all breaker state. For tests only.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	state = make(map[string]*entry)
}
