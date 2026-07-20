package modelroute

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResetRouteToUnknownClearsOnlyTargetRuntimeState(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()
	GlobalLeases.ClearAll()
	GlobalCalibrationPersister.mu.Lock()
	GlobalCalibrationPersister.dirty = make(map[string]struct{})
	GlobalCalibrationPersister.mu.Unlock()

	target := MakeMetricsKey(21, "effective")
	otherModel := MakeMetricsKey(21, "other")
	otherChannel := MakeMetricsKey(22, "effective")
	persistedScore := 0.3
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: target.ChannelID, EffectiveModel: target.EffectiveModel,
		RouteState: string(model.RouteOpen), LastErrorClass: string(model.ErrorDeterministic),
		BackoffLevel: 4, ExperienceScore: &persistedScore, ProductionSampleCount: 2,
	}))
	runtimeScore := 0.87
	runtime := &model.ChannelModelMetrics{
		ChannelID: target.ChannelID, EffectiveModel: target.EffectiveModel,
		RouteState: string(model.RouteOpen), LastErrorClass: string(model.ErrorDeterministic),
		BackoffLevel: 4, ExperienceScore: &runtimeScore, ProductionSampleCount: 12,
	}
	GlobalMetricsRuntime.Put(runtime)
	GlobalMetricsRuntime.recordTempFailure(target)
	GlobalCalibrationPersister.MarkDirty(target)
	GlobalRoles.Set(target, model.RolePrimary)
	otherModelRuntime := &model.ChannelModelMetrics{
		ChannelID: otherModel.ChannelID, EffectiveModel: otherModel.EffectiveModel,
		RouteState: string(model.RouteHealthy),
	}
	otherChannelRuntime := &model.ChannelModelMetrics{
		ChannelID: otherChannel.ChannelID, EffectiveModel: otherChannel.EffectiveModel,
		RouteState: string(model.RouteHealthy),
	}
	GlobalMetricsRuntime.Put(otherModelRuntime)
	GlobalMetricsRuntime.Put(otherChannelRuntime)
	GlobalMetricsRuntime.recordTempFailure(otherModel)
	GlobalMetricsRuntime.recordTempFailure(otherChannel)
	GlobalCalibrationPersister.MarkDirty(otherModel)
	GlobalCalibrationPersister.MarkDirty(otherChannel)
	GlobalRoles.Set(otherModel, model.RolePrimary)
	StoreRoutePlan(&model.RoutePlan{RequestedModel: "request-a"})
	StoreRoutePlan(&model.RoutePlan{RequestedModel: "request-b"})
	StoreRoutePlan(&model.RoutePlan{RequestedModel: "unrelated"})
	GlobalLeases.SetLease(&OverflowLease{
		RequestedModel: "request-a",
		Candidate: model.ResolvedRouteCandidate{
			ChannelID: target.ChannelID, EffectiveModel: target.EffectiveModel,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	GlobalLeases.SetLease(&OverflowLease{
		RequestedModel: "request-b",
		Candidate: model.ResolvedRouteCandidate{
			ChannelID: otherChannel.ChannelID, EffectiveModel: otherChannel.EffectiveModel,
		},
		ExpiresAt: time.Now().Add(time.Minute),
	})

	require.NoError(t, ResetRouteToUnknown(target.ChannelID, target.EffectiveModel, []string{
		"request-a", "request-b", "request-a",
	}))

	stored, err := model.GetChannelModelMetrics(target.ChannelID, target.EffectiveModel)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.RouteUnknown, stored.State())
	assert.Zero(t, stored.BackoffLevel)
	assert.Nil(t, stored.CooldownUntil)
	assert.Empty(t, stored.LastErrorClass)
	assert.Equal(t, int64(12), stored.ProductionSampleCount)
	require.NotNil(t, stored.ExperienceScore)
	assert.InDelta(t, runtimeScore, *stored.ExperienceScore, 1e-9)
	assert.Nil(t, GlobalMetricsRuntime.Get(target))
	assert.Zero(t, GlobalMetricsRuntime.tempFailuresInWindow(target))
	assert.Equal(t, model.RoleNone, GlobalRoles.Get(target))
	assert.Same(t, otherModelRuntime, GlobalMetricsRuntime.Get(otherModel))
	assert.Same(t, otherChannelRuntime, GlobalMetricsRuntime.Get(otherChannel))
	assert.Equal(t, 1, GlobalMetricsRuntime.tempFailuresInWindow(otherModel))
	assert.Equal(t, 1, GlobalMetricsRuntime.tempFailuresInWindow(otherChannel))
	assert.Equal(t, model.RolePrimary, GlobalRoles.Get(otherModel))
	assert.Equal(t, model.RoleOverflow, GlobalRoles.Get(otherChannel))
	GlobalCalibrationPersister.mu.Lock()
	_, dirty := GlobalCalibrationPersister.dirty[target.String()]
	_, otherModelDirty := GlobalCalibrationPersister.dirty[otherModel.String()]
	_, otherChannelDirty := GlobalCalibrationPersister.dirty[otherChannel.String()]
	GlobalCalibrationPersister.mu.Unlock()
	assert.False(t, dirty)
	assert.True(t, otherModelDirty)
	assert.True(t, otherChannelDirty)
	assert.Nil(t, GetCachedRoutePlan("request-a"))
	assert.Nil(t, GetCachedRoutePlan("request-b"))
	assert.NotNil(t, GetCachedRoutePlan("unrelated"))
	assert.Nil(t, GlobalLeases.GetValidOverflowLease("request-a"))
	lease := GlobalLeases.GetValidOverflowLease("request-b")
	require.NotNil(t, lease)
	assert.Equal(t, otherChannel.ChannelID, lease.Candidate.ChannelID)
}

func TestResetRouteToUnknownMissingRowDoesNotEvictRuntime(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	mk := MakeMetricsKey(404, "missing")
	runtime := &model.ChannelModelMetrics{
		ChannelID: mk.ChannelID, EffectiveModel: mk.EffectiveModel,
		RouteState: string(model.RouteOpen),
	}
	GlobalMetricsRuntime.Put(runtime)

	err := ResetRouteToUnknown(mk.ChannelID, mk.EffectiveModel, []string{"request"})
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.Same(t, runtime, GlobalMetricsRuntime.Get(mk))
}

func TestApplyTransitionReloadsResetStateBeforeInFlightOutcome(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	mk := MakeMetricsKey(31, "effective")
	stale := &model.ChannelModelMetrics{
		ChannelID: mk.ChannelID, EffectiveModel: mk.EffectiveModel,
		RouteState: string(model.RouteOpen), BackoffLevel: 3,
	}
	require.NoError(t, model.UpsertChannelModelMetrics(stale))
	GlobalMetricsRuntime.Put(stale)
	require.NoError(t, ResetRouteToUnknown(mk.ChannelID, mk.EffectiveModel, nil))

	assert.True(t, ApplyTransition(stale, EventProductionSuccess, 0))
	assert.Equal(t, model.RouteHealthy, stale.State())
	assert.Zero(t, stale.BackoffLevel)
	stored, err := model.GetChannelModelMetrics(mk.ChannelID, mk.EffectiveModel)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.RouteHealthy, stored.State())

	require.NoError(t, ResetRouteToUnknown(mk.ChannelID, mk.EffectiveModel, nil))
	assert.True(t, ApplyTransition(stale, EventDeterministicFail, 0))
	assert.Equal(t, model.RouteOpen, stale.State())
	assert.Positive(t, stale.BackoffLevel)
	stored, err = model.GetChannelModelMetrics(mk.ChannelID, mk.EffectiveModel)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.RouteOpen, stored.State())
	assert.Positive(t, stored.BackoffLevel)
}
