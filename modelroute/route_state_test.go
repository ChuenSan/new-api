package modelroute

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
)

func withFrozenNow(t *testing.T, ts time.Time) {
	t.Helper()
	prev := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = prev })
}

func withCircuitBreakerThreshold(t *testing.T, threshold int) {
	t.Helper()
	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = threshold
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })
}

func TestProductionFailuresShareConfiguredThreshold(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()
	withCircuitBreakerThreshold(t, 4)

	m := &model.ChannelModelMetrics{
		ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	for i, event := range []TransitionEvent{
		EventDeterministicFail, EventRateLimited, EventTemporaryFail, EventTemporaryFail,
	} {
		ApplyTransition(m, event, 120)
		assert.Equal(t, i+1, m.ConsecutiveFailures)
		if i < 3 {
			assert.Equal(t, model.RouteHealthy, m.State())
		}
	}
	assert.Equal(t, model.RouteOpen, m.State())
	assert.Equal(t, 4, m.ConsecutiveFailures)
	assert.Zero(t, m.ConsecutiveRateLimitFailures)
}

func TestProductionSuccessResetsConsecutiveFailures(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	withCircuitBreakerThreshold(t, 3)

	m := &model.ChannelModelMetrics{
		ChannelID: 2, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	ApplyTransition(m, EventDeterministicFail, 0)
	ApplyTransition(m, EventTemporaryFail, 0)
	assert.Equal(t, 2, m.ConsecutiveFailures)

	ApplyTransition(m, EventProductionSuccess, 0)
	assert.Equal(t, 0, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, m.State())

	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, 1, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, m.State())
}

func TestProductionFailureThreshold999(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	withCircuitBreakerThreshold(t, 999)

	m := &model.ChannelModelMetrics{
		ChannelID: 3, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	for i := 0; i < 998; i++ {
		ApplyTransition(m, EventTemporaryFail, 0)
	}
	assert.Equal(t, 998, m.ConsecutiveFailures)
	assert.NotEqual(t, model.RouteOpen, m.State())

	ApplyTransition(m, EventDeterministicFail, 0)
	assert.Equal(t, 999, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteOpen, m.State())
}

func TestRecoverFlow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()

	m := &model.ChannelModelMetrics{
		ChannelID: 3, EffectiveModel: "m", RouteState: string(model.RouteOpen),
	}
	m.SetCooldownUntil(base.Add(-1 * time.Second))
	assert.True(t, MaybeAdvanceCooldown(m))
	assert.Equal(t, model.RouteProbing, m.State())

	ApplyTransition(m, EventProbeSuccess, 0)
	assert.Equal(t, model.RouteRecovering, m.State())
	assert.Equal(t, 1, m.RecoverSuccessCount)

	ApplyTransition(m, EventProbeSuccess, 0)
	assert.Equal(t, model.RouteRecovering, m.State())
	ApplyTransition(m, EventProbeSuccess, 0)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Equal(t, 0, m.BackoffLevel)
}

func TestProductionSuccessFromUnknown(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	m := &model.ChannelModelMetrics{
		ChannelID: 4, EffectiveModel: "m", RouteState: string(model.RouteUnknown),
	}
	ApplyTransition(m, EventProductionSuccess, 0)
	assert.Equal(t, model.RouteHealthy, m.State())
}

func TestManualDisableAndRestore(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()
	m := &model.ChannelModelMetrics{
		ChannelID: 5, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	mk := m.MetricsKey()
	GlobalRoles.Set(mk, model.RolePrimary)
	ApplyTransition(m, EventManualDisable, 0)
	assert.Equal(t, model.RouteManuallyDisabled, m.State())
	assert.Equal(t, model.RoleNone, GlobalRoles.Get(mk))

	ApplyTransition(m, EventRestoreAuto, 0)
	assert.Equal(t, model.RouteProbing, m.State())
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		class  model.ErrorClass
		event  TransitionEvent
	}{
		{status: 0, class: model.ErrorTemporary, event: EventTemporaryFail},
		{status: 201, class: model.ErrorTemporary, event: EventTemporaryFail},
		{status: 204, class: model.ErrorTemporary, event: EventTemporaryFail},
		{status: 401, class: model.ErrorDeterministic, event: EventDeterministicFail},
		{status: 403, class: model.ErrorDeterministic, event: EventDeterministicFail},
		{status: 404, class: model.ErrorDeterministic, event: EventDeterministicFail},
		{status: 429, class: model.ErrorTemporary, event: EventRateLimited},
		{status: 500, class: model.ErrorTemporary, event: EventTemporaryFail},
		{status: 503, class: model.ErrorTemporary, event: EventTemporaryFail},
	}
	for _, tt := range tests {
		c, e := ClassifyHTTPStatus(tt.status)
		assert.Equal(t, tt.class, c, "status=%d", tt.status)
		assert.Equal(t, tt.event, e, "status=%d", tt.status)
		assert.NotEqual(t, EventProductionSuccess, e, "status=%d", tt.status)
	}

	c, e := ClassifyHTTPStatus(200)
	assert.Equal(t, EventProductionSuccess, e)
	assert.Empty(t, c)
}

func TestIsRouteStale(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	// first_standby: max(3*120s, 30m) = 30m
	assert.Equal(t, 30*time.Minute, StaleAfter(true))
	// other: max(3*600s, 30m) = 30m
	assert.Equal(t, 30*time.Minute, StaleAfter(false))

	old := base.Add(-31 * time.Minute).Unix()
	m := &model.ChannelModelMetrics{
		ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteHealthy), LastSuccessAt: &old,
	}
	assert.True(t, IsRouteStale(m, true))

	fresh := base.Add(-5 * time.Minute).Unix()
	m.LastSuccessAt = &fresh
	assert.False(t, IsRouteStale(m, true))

	// OPEN never stale-soft-mark for takeover path
	m.RouteState = string(model.RouteOpen)
	m.LastSuccessAt = &old
	assert.False(t, IsRouteStale(m, true))
}

func TestIsProductiveState(t *testing.T) {
	assert.True(t, IsProductiveState(model.RouteHealthy))
	assert.True(t, IsProductiveState(model.RouteRecovering))
	assert.True(t, IsProductiveState(model.RouteUnknown))
	assert.False(t, IsProductiveState(model.RouteOpen))
	assert.False(t, IsProductiveState(model.RouteRateLimited))
	assert.False(t, IsProductiveState(model.RouteProbing))
}

func TestRateLimitEventUsesSharedFailureCounter(t *testing.T) {
	withFrozenNow(t, time.Unix(1_700_000_000, 0))
	GlobalMetricsRuntime.Clear()
	withCircuitBreakerThreshold(t, 3)
	m := &model.ChannelModelMetrics{ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteHealthy)}
	ApplyTransition(m, EventRateLimited, 120)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Nil(t, m.CooldownUntil)
	assert.Equal(t, 1, m.ConsecutiveFailures)
	assert.Zero(t, m.ConsecutiveRateLimitFailures)
}

func TestFailuresTripOpenAtConfiguredThreshold(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()

	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = 3
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })

	m := &model.ChannelModelMetrics{
		ChannelID: 6, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	ApplyTransition(m, EventRateLimited, 0)
	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Equal(t, 2, m.ConsecutiveFailures)

	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteOpen, m.State())
	assert.Equal(t, 3, m.ConsecutiveFailures)
}

func TestFailureSuccessResetsConsecutiveFailures(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()

	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = 3
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })

	m := &model.ChannelModelMetrics{
		ChannelID: 7, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	ApplyTransition(m, EventRateLimited, 0)
	ApplyTransition(m, EventRateLimited, 0)
	ApplyTransition(m, EventProductionSuccess, 0)
	assert.Equal(t, 0, m.ConsecutiveFailures)

	ApplyTransition(m, EventRateLimited, 0)
	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteHealthy, m.State())
	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteOpen, m.State())
}

func TestMixedFailuresShareRateLimitThreshold(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()

	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = 4
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })

	m := &model.ChannelModelMetrics{
		ChannelID: 8, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	ApplyTransition(m, EventTemporaryFail, 0)
	ApplyTransition(m, EventRateLimited, 0)
	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Equal(t, 3, m.ConsecutiveFailures)
	assert.Zero(t, m.ConsecutiveRateLimitFailures)

	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteOpen, m.State())
	assert.Equal(t, 4, m.ConsecutiveFailures)
}

func TestFailureCounterIsScopedPerRouteAndSafeForConcurrentUpdates(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()

	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = 3
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })

	m1 := &model.ChannelModelMetrics{
		ChannelID: 9, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	m2 := &model.ChannelModelMetrics{
		ChannelID: 10, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ApplyTransition(m1, EventRateLimited, 0)
		}()
	}
	wg.Wait()

	assert.Equal(t, model.RouteOpen, m1.State())
	assert.Equal(t, 3, m1.ConsecutiveFailures)
	assert.Zero(t, m1.ConsecutiveRateLimitFailures)
	ApplyTransition(m2, EventRateLimited, 0)
	assert.Equal(t, model.RouteHealthy, m2.State())
	assert.Equal(t, 1, m2.ConsecutiveFailures)
	assert.Zero(t, m2.ConsecutiveRateLimitFailures)
}

func TestFailureThresholdUsesRouteOverrideBeforeGlobalFallback(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	withFrozenNow(t, base)
	GlobalMetricsRuntime.Clear()

	settings := operation_setting.GetModelRouteSetting()
	original := settings.RateLimitCircuitBreakerThreshold
	settings.RateLimitCircuitBreakerThreshold = 3
	t.Cleanup(func() { settings.RateLimitCircuitBreakerThreshold = original })
	invalidOverride := 2
	assert.Equal(t, 3, GetRateLimitCircuitBreakerThreshold(&model.ChannelModelMetrics{
		RateLimitCircuitBreakerThreshold: &invalidOverride,
	}))

	override := 5
	m := &model.ChannelModelMetrics{
		ChannelID: 11, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
		RateLimitCircuitBreakerThreshold: &override,
	}
	for range 4 {
		ApplyTransition(m, EventRateLimited, 0)
	}
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Equal(t, 4, m.ConsecutiveFailures)
	assert.Zero(t, m.ConsecutiveRateLimitFailures)
	ApplyTransition(m, EventRateLimited, 0)
	assert.Equal(t, model.RouteOpen, m.State())

	fallback := &model.ChannelModelMetrics{
		ChannelID: 12, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	ApplyTransition(fallback, EventRateLimited, 0)
	ApplyTransition(fallback, EventRateLimited, 0)
	assert.Equal(t, model.RouteHealthy, fallback.State())
	ApplyTransition(fallback, EventRateLimited, 0)
	assert.Equal(t, model.RouteOpen, fallback.State())
}
