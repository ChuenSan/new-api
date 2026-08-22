package modelroute

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// now is overridable for deterministic tests.
var now = time.Now

// RuntimeRoleStore holds process-local RouteRole per MetricsKey (PRD §8.2 — not persisted).
type RuntimeRoleStore struct {
	mu    sync.RWMutex
	roles map[string]model.RouteRole
}

// GlobalRoles is the process-local role map.
var GlobalRoles = &RuntimeRoleStore{roles: make(map[string]model.RouteRole)}

func (s *RuntimeRoleStore) Get(mk model.MetricsKey) model.RouteRole {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.roles[mk.String()]; ok {
		return r
	}
	return model.RoleNone
}

func (s *RuntimeRoleStore) Set(mk model.MetricsKey, role model.RouteRole) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == model.RoleNone {
		delete(s.roles, mk.String())
		return
	}
	s.roles[mk.String()] = role
}

func (s *RuntimeRoleStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles = make(map[string]model.RouteRole)
}

// RuntimeMetricsCache holds hot metrics copies for state transitions without always hitting DB.
type RuntimeMetricsCache struct {
	mu        sync.RWMutex
	data      map[string]*model.ChannelModelMetrics
	resetKeys map[string]struct{}
}

// GlobalMetricsRuntime is the process-local metrics overlay.
var GlobalMetricsRuntime = &RuntimeMetricsCache{
	data:      make(map[string]*model.ChannelModelMetrics),
	resetKeys: make(map[string]struct{}),
}

func (c *RuntimeMetricsCache) Get(mk model.MetricsKey) *model.ChannelModelMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[mk.String()]
}

func (c *RuntimeMetricsCache) Put(m *model.ChannelModelMetrics) {
	if m == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// shallow copy pointer store; callers own mutation under external discipline
	key := m.MetricsKey().String()
	c.data[key] = m
	delete(c.resetKeys, key)
}

func (c *RuntimeMetricsCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*model.ChannelModelMetrics)
	c.resetKeys = make(map[string]struct{})
}

func (c *RuntimeMetricsCache) Delete(mk model.MetricsKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := mk.String()
	delete(c.data, key)
	c.resetKeys[key] = struct{}{}
}

func (c *RuntimeMetricsCache) needsRefresh(mk model.MetricsKey) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.resetKeys[mk.String()]
	return ok
}

// LoadOrEnsureMetrics loads DB row into runtime cache.
func LoadOrEnsureMetrics(channelID int64, effectiveModel string) (*model.ChannelModelMetrics, error) {
	mk := MakeMetricsKey(channelID, effectiveModel)
	lock := metricsLockFor(mk)
	lock.Lock()
	defer lock.Unlock()
	return loadOrEnsureMetricsLocked(channelID, effectiveModel)
}

func loadOrEnsureMetricsLocked(channelID int64, effectiveModel string) (*model.ChannelModelMetrics, error) {
	mk := MakeMetricsKey(channelID, effectiveModel)
	if m := GlobalMetricsRuntime.Get(mk); m != nil {
		return m, nil
	}
	m, err := model.EnsureChannelModelMetrics(channelID, effectiveModel)
	if err != nil {
		return nil, err
	}
	GlobalMetricsRuntime.Put(m)
	return m, nil
}

func refreshMetricsLocked(m *model.ChannelModelMetrics) *model.ChannelModelMetrics {
	if m == nil {
		return nil
	}
	mk := m.MetricsKey()
	if current := GlobalMetricsRuntime.Get(mk); current != nil {
		if current != m {
			*m = *current
		}
		return m
	}
	if GlobalMetricsRuntime.needsRefresh(mk) {
		persisted, err := model.GetChannelModelMetrics(mk.ChannelID, mk.EffectiveModel)
		if err == nil && persisted != nil {
			*m = *persisted
		}
	}
	return m
}

// TransitionEvent drives the RouteState machine (PRD §8.1 / §24 / §25 / §26).
type TransitionEvent string

const (
	EventProductionSuccess TransitionEvent = "production_success"
	EventRateLimited       TransitionEvent = "rate_limited"
	EventDeterministicFail TransitionEvent = "deterministic_fail"
	EventTemporaryFail     TransitionEvent = "temporary_fail"
	EventProbeSuccess      TransitionEvent = "probe_success"
	EventProbeFail         TransitionEvent = "probe_fail"
	EventCooldownElapsed   TransitionEvent = "cooldown_elapsed"
	EventManualDisable     TransitionEvent = "manual_disable"
	EventRestoreAuto       TransitionEvent = "restore_auto"
	EventForceProbe        TransitionEvent = "force_probe"
	EventTripOpen          TransitionEvent = "trip_open"
)

// ApplyTransition mutates metrics state according to PRD rules. Returns true if state changed.
func ApplyTransition(m *model.ChannelModelMetrics, event TransitionEvent, retryAfterSec int) bool {
	if m == nil {
		return false
	}
	lock := metricsLockFor(m.MetricsKey())
	lock.Lock()
	defer lock.Unlock()
	return applyTransitionLocked(refreshMetricsLocked(m), event, retryAfterSec)
}

func applyTransitionLocked(m *model.ChannelModelMetrics, event TransitionEvent, retryAfterSec int) bool {
	if m == nil {
		return false
	}
	before := m.State()
	mk := m.MetricsKey()
	ts := now().Unix()

	switch event {
	case EventManualDisable:
		m.ConsecutiveRateLimitFailures = 0
		m.SetState(model.RouteManuallyDisabled)
		m.SetCooldownUntil(time.Time{})
		GlobalRoles.Set(mk, model.RoleNone)

	case EventRestoreAuto:
		if before == model.RouteManuallyDisabled {
			m.ConsecutiveRateLimitFailures = 0
			m.SetState(model.RouteProbing)
			m.BackoffLevel = 0
			m.SetCooldownUntil(time.Time{})
		}

	case EventForceProbe:
		if before != model.RouteManuallyDisabled {
			m.SetState(model.RouteProbing)
			m.SetCooldownUntil(time.Time{})
		}

	case EventCooldownElapsed:
		if before == model.RouteRateLimited || before == model.RouteOpen {
			m.SetState(model.RouteProbing)
			m.SetCooldownUntil(time.Time{})
		}

	case EventRateLimited, EventDeterministicFail, EventTemporaryFail:
		// These legacy event names preserve error classification only. All
		// production failures share the same consecutive-failure threshold.
		if event == EventDeterministicFail {
			m.SetLastErrorClass(model.ErrorDeterministic)
		} else {
			m.SetLastErrorClass(model.ErrorTemporary)
		}
		m.ConsecutiveFailures++
		m.ConsecutiveRateLimitFailures = 0
		m.LastFailureAt = &ts
		if m.ConsecutiveFailures >= GetRateLimitCircuitBreakerThreshold(m) {
			openCircuit(m, 0)
			GlobalRoles.Set(mk, model.RoleNone)
		}

	case EventTripOpen:
		m.ConsecutiveRateLimitFailures = 0
		openCircuit(m, 0)
		GlobalRoles.Set(mk, model.RoleNone)

	case EventProbeSuccess:
		m.ConsecutiveRateLimitFailures = 0
		m.LastProbeAt = &ts
		m.LastSuccessAt = &ts
		switch before {
		case model.RouteProbing, model.RouteOpen, model.RouteRateLimited, model.RouteUnknown:
			m.SetState(model.RouteRecovering)
			m.RecoverSuccessCount = 1
			m.ConsecutiveFailures = 0
			m.SetCooldownUntil(time.Time{})
		case model.RouteRecovering:
			m.RecoverSuccessCount++
			if m.RecoverSuccessCount >= model.DefaultRecoverSuccessThreshold {
				m.SetState(model.RouteHealthy)
				m.BackoffLevel = 0
				m.RecoverSuccessCount = 0
			}
		}

	case EventProbeFail:
		m.ConsecutiveRateLimitFailures = 0
		m.LastProbeAt = &ts
		m.LastFailureAt = &ts
		// stay PROBING or re-open with higher backoff depending on error class set by caller
		if m.GetLastErrorClass() == model.ErrorDeterministic {
			openCircuit(m, 0)
		} else if before == model.RouteProbing || before == model.RouteRecovering {
			// temporary probe fail: return to OPEN with next backoff
			openCircuit(m, m.BackoffLevel)
		}

	case EventProductionSuccess:
		m.LastSuccessAt = &ts
		m.LastRequestAt = &ts
		m.ConsecutiveFailures = 0
		m.ConsecutiveRateLimitFailures = 0
		switch before {
		case model.RouteUnknown, model.RouteProbing:
			m.SetState(model.RouteHealthy)
			m.BackoffLevel = 0
			m.RecoverSuccessCount = 0
		case model.RouteRecovering:
			m.RecoverSuccessCount++
			if m.RecoverSuccessCount >= model.DefaultRecoverSuccessThreshold {
				m.SetState(model.RouteHealthy)
				m.BackoffLevel = 0
				m.RecoverSuccessCount = 0
			}
		case model.RouteHealthy:
			// stay
		}
	}

	after := m.State()
	if after != before {
		m.UpdatedAt = now().Unix()
		GlobalMetricsRuntime.Put(m)
		// critical transitions persist immediately (PRD §17)
		switch after {
		case model.RouteOpen, model.RouteRateLimited, model.RouteManuallyDisabled,
			model.RouteHealthy, model.RouteProbing, model.RouteRecovering:
			_ = GlobalCalibrationPersister.snapshotCriticalLocked(m)
		default:
			GlobalCalibrationPersister.MarkDirty(mk)
		}
		return true
	}
	GlobalMetricsRuntime.Put(m)
	GlobalCalibrationPersister.MarkDirty(mk)
	return false
}

func openCircuit(m *model.ChannelModelMetrics, levelHint int) {
	m.SetState(model.RouteOpen)
	level := levelHint
	if level <= 0 {
		level = m.BackoffLevel
	}
	if level < 0 {
		level = 0
	}
	if level >= len(model.DefaultOpenBackoffSeconds) {
		level = len(model.DefaultOpenBackoffSeconds) - 1
	}
	cd := model.DefaultOpenBackoffSeconds[level]
	m.SetCooldownUntil(now().Add(time.Duration(cd) * time.Second))
	if m.BackoffLevel < len(model.DefaultOpenBackoffSeconds)-1 {
		m.BackoffLevel = level + 1
	} else {
		m.BackoffLevel = level
	}
	ts := now().Unix()
	m.LastFailureAt = &ts
}

// MaybeAdvanceCooldown moves RATE_LIMITED/OPEN → PROBING when cooldown elapsed (PRD §26).
func MaybeAdvanceCooldown(m *model.ChannelModelMetrics) bool {
	if m == nil {
		return false
	}
	lock := metricsLockFor(m.MetricsKey())
	lock.Lock()
	defer lock.Unlock()
	return maybeAdvanceCooldownLocked(refreshMetricsLocked(m))
}

func maybeAdvanceCooldownLocked(m *model.ChannelModelMetrics) bool {
	if m == nil {
		return false
	}
	st := m.State()
	if st != model.RouteRateLimited && st != model.RouteOpen {
		return false
	}
	until := m.CooldownUntilTime()
	if until.IsZero() || !now().Before(until) {
		return applyTransitionLocked(m, EventCooldownElapsed, 0)
	}
	return false
}

// isSuccessfulProductionResult is the single production success predicate.
func isSuccessfulProductionResult(success bool, statusCode int, streamInterrupted bool) bool {
	return success && statusCode == http.StatusOK && !streamInterrupted
}

// classifyProductionFailure keeps error classes for metrics/display while
// ensuring a failed request with status 200 cannot become a success event.
func classifyProductionFailure(status int) (model.ErrorClass, TransitionEvent) {
	class, event := ClassifyHTTPStatus(status)
	if event == EventProductionSuccess {
		return model.ErrorTemporary, EventTemporaryFail
	}
	return class, event
}

// ClassifyHTTPStatus maps status to ErrorClass / events. HTTP 200 is the only
// successful status; every other status, including 0, is a failure.
func ClassifyHTTPStatus(status int) (model.ErrorClass, TransitionEvent) {
	switch {
	case status == http.StatusOK:
		return "", EventProductionSuccess
	case status == http.StatusTooManyRequests:
		return model.ErrorTemporary, EventRateLimited
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
		return model.ErrorDeterministic, EventDeterministicFail
	case status >= 500:
		return model.ErrorTemporary, EventTemporaryFail
	case status >= 400:
		// Other 4xx responses remain deterministic for metrics/display only.
		return model.ErrorDeterministic, EventDeterministicFail
	default:
		return model.ErrorTemporary, EventTemporaryFail
	}
}

// IsProductiveState reports whether state may serve production traffic.
func IsProductiveState(s model.RouteState) bool {
	switch s {
	case model.RouteHealthy, model.RouteRecovering, model.RouteUnknown:
		return true
	default:
		return false
	}
}

// IsInCooldown reports cooldown still active.
func IsInCooldown(m *model.ChannelModelMetrics) bool {
	if m == nil {
		return false
	}
	until := m.CooldownUntilTime()
	return !until.IsZero() && now().Before(until)
}
