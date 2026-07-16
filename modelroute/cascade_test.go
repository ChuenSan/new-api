package modelroute

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCascadeMetricsForChannelStatusDisable(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()

	const chID int64 = 101
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: chID, EffectiveModel: "m-a", RouteState: string(model.RouteHealthy),
	}))
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: chID, EffectiveModel: "m-b", RouteState: string(model.RouteOpen),
	}))
	// other channel should not be touched
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: 999, EffectiveModel: "m-x", RouteState: string(model.RouteHealthy),
	}))

	n, err := CascadeMetricsForChannelStatus(chID, common.ChannelStatusManuallyDisabled)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	a, err := model.GetChannelModelMetrics(chID, "m-a")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, string(model.RouteManuallyDisabled), a.RouteState)

	b, err := model.GetChannelModelMetrics(chID, "m-b")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, string(model.RouteManuallyDisabled), b.RouteState)

	x, err := model.GetChannelModelMetrics(999, "m-x")
	require.NoError(t, err)
	require.NotNil(t, x)
	assert.Equal(t, string(model.RouteHealthy), x.RouteState)
}

func TestCascadeMetricsForChannelStatusEnableRestore(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	GlobalRoles.Clear()

	const chID int64 = 102
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: chID, EffectiveModel: "m-a", RouteState: string(model.RouteManuallyDisabled),
	}))
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: chID, EffectiveModel: "m-b", RouteState: string(model.RouteHealthy),
	}))

	n, err := CascadeMetricsForChannelStatus(chID, common.ChannelStatusEnabled)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	a, err := model.GetChannelModelMetrics(chID, "m-a")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, string(model.RouteProbing), a.RouteState)

	// restore_auto only affects MANUALLY_DISABLED; HEALTHY stays
	b, err := model.GetChannelModelMetrics(chID, "m-b")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, string(model.RouteHealthy), b.RouteState)
}

func TestCascadeMetricsForChannelStatusAutoDisabledNoop(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()

	const chID int64 = 103
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: chID, EffectiveModel: "m-a", RouteState: string(model.RouteHealthy),
	}))

	n, err := CascadeMetricsForChannelStatus(chID, common.ChannelStatusAutoDisabled)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	a, err := model.GetChannelModelMetrics(chID, "m-a")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, string(model.RouteHealthy), a.RouteState)
}

func TestCascadeMetricsForChannelStatusEmpty(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()

	n, err := CascadeMetricsForChannelStatus(404, common.ChannelStatusManuallyDisabled)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	n, err = CascadeMetricsForChannelStatus(404, common.ChannelStatusEnabled)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
