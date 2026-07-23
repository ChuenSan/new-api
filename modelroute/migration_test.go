package modelroute

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateToModelPriority(t *testing.T) {
	clearRouteTables(t)
	InvalidateAllRoutePlans()
	SetRoutingPriorityMode(model.RoutingPriorityModeChannel)

	pri := int64(42)
	w := uint(10)
	mapping := `{"gpt-a":"eff-a"}`
	ch := &model.Channel{
		Id: 11, Models: "gpt-a,gpt-b", ModelMapping: &mapping,
		Priority: &pri, Weight: &w, Status: common.ChannelStatusEnabled,
		Key: "k", Name: "c11",
	}
	require.NoError(t, model.DB.Create(ch).Error)

	res, err := MigrateToModelPriority()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Greater(t, res.PoliciesTouched, 0)
	assert.Greater(t, res.MetricsTouched, 0)
	assert.Equal(t, 1, res.ChannelsZeroed)
	assert.True(t, IsModelPriorityMode())

	// channel zeroed
	got, err := model.GetChannelById(11, true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.GetPriority())
	assert.Equal(t, 0, got.GetWeight())

	// policy has initial manual_priority from old channel priority
	pol, err := model.GetChannelModelPolicy(11, "gpt-a")
	require.NoError(t, err)
	require.NotNil(t, pol)
	assert.Equal(t, 42, pol.ManualPriority)

	met, err := model.GetChannelModelMetrics(11, "eff-a")
	require.NoError(t, err)
	require.NotNil(t, met)
}

// Existing zero-priority policies (lazy / incomplete prior migrate) must absorb
// channel priority on re-migrate before channel P/W is zeroed.
func TestMigrateToModelPrioritySeedsExistingZeroPolicies(t *testing.T) {
	clearRouteTables(t)
	InvalidateAllRoutePlans()
	SetRoutingPriorityMode(model.RoutingPriorityModeChannel)

	pri := int64(77)
	w := uint(3)
	ch := &model.Channel{
		Id: 22, Models: "gpt-seed", Priority: &pri, Weight: &w,
		Status: common.ChannelStatusEnabled, Key: "k", Name: "c22",
	}
	require.NoError(t, model.DB.Create(ch).Error)
	require.NoError(t, model.UpsertChannelModelPolicy(&model.ChannelModelPolicy{
		ChannelID: 22, RequestedModel: "gpt-seed", ManualPriority: 0,
		Enabled: true, Source: model.PolicySourceLazyCreated,
	}))

	res, err := MigrateToModelPriority()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.GreaterOrEqual(t, res.PoliciesSeeded, 1)
	assert.Equal(t, 1, res.ChannelsZeroed)
	assert.Equal(t, model.RoutingPriorityModeModel, res.Mode)
	assert.True(t, IsModelPriorityMode())

	pol, err := model.GetChannelModelPolicy(22, "gpt-seed")
	require.NoError(t, err)
	require.NotNil(t, pol)
	assert.Equal(t, 77, pol.ManualPriority)

	// idempotent re-run: no channel left to zero; priority stays
	res2, err := MigrateToModelPriority()
	require.NoError(t, err)
	require.NotNil(t, res2)
	assert.Equal(t, 0, res2.ChannelsZeroed)
	pol2, err := model.GetChannelModelPolicy(22, "gpt-seed")
	require.NoError(t, err)
	require.NotNil(t, pol2)
	assert.Equal(t, 77, pol2.ManualPriority)
}

func TestMigrateToModelPriorityPreservesNonZeroManualPriority(t *testing.T) {
	clearRouteTables(t)
	InvalidateAllRoutePlans()
	SetRoutingPriorityMode(model.RoutingPriorityModeChannel)

	pri := int64(50)
	ch := &model.Channel{
		Id: 33, Models: "gpt-keep", Priority: &pri, Status: common.ChannelStatusEnabled,
		Key: "k", Name: "c33",
	}
	require.NoError(t, model.DB.Create(ch).Error)
	require.NoError(t, model.UpsertChannelModelPolicy(&model.ChannelModelPolicy{
		ChannelID: 33, RequestedModel: "gpt-keep", ManualPriority: 12,
		Enabled: true, Source: model.PolicySourceConfigured,
	}))

	_, err := MigrateToModelPriority()
	require.NoError(t, err)

	pol, err := model.GetChannelModelPolicy(33, "gpt-keep")
	require.NoError(t, err)
	require.NotNil(t, pol)
	assert.Equal(t, 12, pol.ManualPriority)
}

func TestResetLearningHelpers(t *testing.T) {
	clearRouteTables(t)
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: 1, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
		ShadowCalibrationJSON: `{"0-1k":{"ratio":1.2,"sample_count":1}}`,
	}))
	succ := 0.9
	m, _ := model.GetChannelModelMetrics(1, "m")
	require.NotNil(t, m)
	m.ProductionSuccessEMA = &succ
	require.NoError(t, model.UpsertChannelModelMetrics(m))

	n, err := ResetRuntimeLearning(1, "m")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	after, _ := model.GetChannelModelMetrics(1, "m")
	require.NotNil(t, after)
	assert.Nil(t, after.ProductionSuccessEMA)
	assert.NotEmpty(t, after.ShadowCalibrationJSON)

	n, err = ResetAllLearning(1, "m")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	after, _ = model.GetChannelModelMetrics(1, "m")
	require.NotNil(t, after)
	assert.Empty(t, after.ShadowCalibrationJSON)
}
