package modelroute

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectColdStartCandidates(t *testing.T) {
	cands := []model.ResolvedRouteCandidate{
		{ChannelID: 1, ManualPriority: 10, Metrics: &model.ChannelModelMetrics{RouteState: string(model.RouteUnknown)}},
		{ChannelID: 2, ManualPriority: 50, Metrics: &model.ChannelModelMetrics{RouteState: string(model.RouteUnknown)}},
		{ChannelID: 3, ManualPriority: 100, Metrics: &model.ChannelModelMetrics{RouteState: string(model.RouteHealthy)}},
	}
	unknown := SelectColdStartCandidates(cands)
	require.Len(t, unknown, 2)
	assert.Equal(t, int64(2), unknown[0].ChannelID) // higher priority first
	assert.Equal(t, int64(1), unknown[1].ChannelID)
}

func TestAssignBootstrapAndPromote(t *testing.T) {
	GlobalRoles.Clear()
	m := &model.ChannelModelMetrics{ChannelID: 7, EffectiveModel: "eff", RouteState: string(model.RouteUnknown)}
	c := &model.ResolvedRouteCandidate{ChannelID: 7, EffectiveModel: "eff", Metrics: m}
	AssignBootstrapRole(c)
	mk := MakeMetricsKey(7, "eff")
	assert.Equal(t, model.RoleBootstrap, GlobalRoles.Get(mk))
	PromoteToPrimary(c)
	assert.Equal(t, model.RolePrimary, GlobalRoles.Get(mk))
	assert.Equal(t, model.RouteHealthy, m.State())
}

func TestTransparentRetryPreByteSwitches(t *testing.T) {
	GlobalRoles.Clear()
	GlobalMetricsRuntime.Clear()
	m1 := &model.ChannelModelMetrics{ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteUnknown)}
	m2 := &model.ChannelModelMetrics{ChannelID: 2, EffectiveModel: "m", RouteState: string(model.RouteUnknown)}
	plan := TransparentRetryPlan{
		Candidates: []model.ResolvedRouteCandidate{
			{ChannelID: 1, EffectiveModel: "m", ManualPriority: 10, Metrics: m1},
			{ChannelID: 2, EffectiveModel: "m", ManualPriority: 5, Metrics: m2},
		},
		ColdStart: true,
	}
	AssignBootstrapRole(&plan.Candidates[0])

	calls := 0
	idx, out, exhausted := RunTransparentRetry(plan, func(c model.ResolvedRouteCandidate, i int) AttemptOutcome {
		calls++
		if i == 0 {
			return ClassifyAttempt(false, 503, false, false)
		}
		return ClassifyAttempt(true, 200, false, false)
	})
	require.Equal(t, 2, calls)
	assert.Equal(t, 1, idx)
	assert.True(t, out.Success)
	assert.False(t, exhausted)
	assert.Equal(t, model.RouteUnknown, m1.State())
	assert.Equal(t, 1, m1.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, m2.State())
	assert.Equal(t, model.RolePrimary, GlobalRoles.Get(MakeMetricsKey(2, "m")))
	// failed bootstrap candidate keeps none/bootstrap cleared of primary
	assert.NotEqual(t, model.RolePrimary, GlobalRoles.Get(MakeMetricsKey(1, "m")))
}

func TestTransparentRetryStopsAfterFirstByte(t *testing.T) {
	GlobalRoles.Clear()
	GlobalMetricsRuntime.Clear()
	m1 := &model.ChannelModelMetrics{ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteHealthy)}
	m2 := &model.ChannelModelMetrics{ChannelID: 2, EffectiveModel: "m", RouteState: string(model.RouteHealthy)}
	plan := TransparentRetryPlan{
		Candidates: []model.ResolvedRouteCandidate{
			{ChannelID: 1, EffectiveModel: "m", Metrics: m1},
			{ChannelID: 2, EffectiveModel: "m", Metrics: m2},
		},
	}
	calls := 0
	idx, out, exhausted := RunTransparentRetry(plan, func(c model.ResolvedRouteCandidate, i int) AttemptOutcome {
		calls++
		return ClassifyAttempt(false, 500, true, true) // stream interrupted after first byte
	})
	assert.Equal(t, 1, calls)
	assert.Equal(t, -1, idx)
	assert.False(t, exhausted)
	assert.False(t, out.Success)
	assert.True(t, out.StreamInterrupted)
	assert.Equal(t, 1, m1.ConsecutiveFailures)
	require.NotNil(t, m1.StreamInterruptionEMA)
	assert.Greater(t, *m1.StreamInterruptionEMA, 0.0)
	// second candidate not tried
	assert.Equal(t, model.RouteHealthy, m2.State())
}

func TestClassifyAttempt(t *testing.T) {
	o := ClassifyAttempt(true, 200, false, false)
	assert.True(t, o.Success)
	assert.Equal(t, EventProductionSuccess, o.Event)

	o = ClassifyAttempt(true, 204, false, false)
	assert.False(t, o.Success)
	assert.Equal(t, EventTemporaryFail, o.Event)

	o = ClassifyAttempt(true, 0, false, false)
	assert.False(t, o.Success)
	assert.Equal(t, EventTemporaryFail, o.Event)

	o = ClassifyAttempt(false, 200, false, false)
	assert.False(t, o.Success)
	assert.Equal(t, EventTemporaryFail, o.Event)

	o = ClassifyAttempt(false, 429, false, false)
	assert.Equal(t, EventRateLimited, o.Event)

	o = ClassifyAttempt(false, 500, true, true)
	assert.False(t, o.Success)
	assert.True(t, o.StreamInterrupted)
	assert.Equal(t, EventTemporaryFail, o.Event)
}

// TestNormalizeAttemptOutcomeCannotBypassFailure guards canonical failure events.
func TestNormalizeAttemptOutcomeCannotBypassFailure(t *testing.T) {
	out := normalizeAttemptOutcome(AttemptOutcome{
		Success:    false,
		StatusCode: http.StatusInternalServerError,
		ErrorClass: model.ErrorTemporary,
		Event:      EventProductionSuccess,
	})
	assert.False(t, out.Success)
	assert.Equal(t, EventTemporaryFail, out.Event)
}
