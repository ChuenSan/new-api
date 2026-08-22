package modelroute

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyProductionOutcomeSuccessAndFail(t *testing.T) {
	clearRouteTables(t)
	GlobalRoles.Clear()
	GlobalMetricsRuntime.Clear()
	SetRoutingPriorityMode(model.RoutingPriorityModeModel)
	withCircuitBreakerThreshold(t, 3)

	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: 11, EffectiveModel: "m", RouteState: string(model.RouteUnknown),
	}))

	ApplyProductionOutcome(ProductionOutcome{
		ChannelID: 11, RequestedModel: "m", Success: true, StatusCode: 200, TTFT: 25 * time.Millisecond,
	})
	m := EnsureRuntimeMetrics(11, "m")
	require.NotNil(t, m)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.NotNil(t, m.ProductionSuccessEMA)
	assert.Equal(t, model.RolePrimary, GlobalRoles.Get(MakeMetricsKey(11, "m")))

	ApplyProductionOutcome(ProductionOutcome{
		ChannelID: 11, RequestedModel: "m", Success: false, StatusCode: 503,
	})
	assert.Equal(t, 1, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.NotNil(t, m.ProductionSuccessEMA)
}

func TestApplyProductionOutcomeRequiresHTTP200AndSharesFailureThreshold(t *testing.T) {
	clearRouteTables(t)
	GlobalRoles.Clear()
	GlobalMetricsRuntime.Clear()
	SetRoutingPriorityMode(model.RoutingPriorityModeModel)
	withCircuitBreakerThreshold(t, 4)

	m := &model.ChannelModelMetrics{
		ChannelID: 12, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	require.NoError(t, model.UpsertChannelModelMetrics(m))
	m = EnsureRuntimeMetrics(12, "m")
	require.NotNil(t, m)

	outcomes := []ProductionOutcome{
		{ChannelID: 12, RequestedModel: "m", Success: false, StatusCode: 401},
		{ChannelID: 12, RequestedModel: "m", Success: false, StatusCode: 429},
		// A 204 is not a successful production response even when the relay
		// completion flag is true.
		{ChannelID: 12, RequestedModel: "m", Success: true, StatusCode: 204},
		{ChannelID: 12, RequestedModel: "m", Success: false, StatusCode: 0},
	}
	for i, outcome := range outcomes[:3] {
		ApplyProductionOutcome(outcome)
		assert.Equal(t, i+1, m.ConsecutiveFailures)
		assert.Equal(t, model.RouteHealthy, m.State())
	}
	ApplyProductionOutcome(outcomes[3])
	assert.Equal(t, 4, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteOpen, m.State())

	// A normal HTTP 200 completion clears the streak, even after OPEN.
	ApplyProductionOutcome(ProductionOutcome{
		ChannelID: 12, RequestedModel: "m", Success: true, StatusCode: 200,
	})
	assert.Zero(t, m.ConsecutiveFailures)

	stream := &model.ChannelModelMetrics{
		ChannelID: 13, EffectiveModel: "m", RouteState: string(model.RouteHealthy),
	}
	require.NoError(t, model.UpsertChannelModelMetrics(stream))
	stream = EnsureRuntimeMetrics(13, "m")
	require.NotNil(t, stream)
	ApplyProductionOutcome(ProductionOutcome{
		ChannelID: 13, RequestedModel: "m", Success: true, StatusCode: 200, StreamInterrupted: true,
	})
	assert.Equal(t, 1, stream.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, stream.State())
}

func TestApplyProductionOutcomeUsesEffectiveModelMetricsKey(t *testing.T) {
	clearRouteTables(t)
	GlobalMetricsRuntime.Clear()
	SetRoutingPriorityMode(model.RoutingPriorityModeModel)
	withCircuitBreakerThreshold(t, 3)

	m := &model.ChannelModelMetrics{
		ChannelID: 14, EffectiveModel: "provider/model", RouteState: string(model.RouteHealthy),
	}
	require.NoError(t, model.UpsertChannelModelMetrics(m))
	requested := &model.ChannelModelMetrics{
		ChannelID: 14, EffectiveModel: "requested", RouteState: string(model.RouteHealthy),
	}
	require.NoError(t, model.UpsertChannelModelMetrics(requested))
	m = EnsureRuntimeMetrics(14, "provider/model")
	require.NotNil(t, m)
	requested = EnsureRuntimeMetrics(14, "requested")
	require.NotNil(t, requested)
	for range 2 {
		ApplyProductionOutcome(ProductionOutcome{
			ChannelID: 14, RequestedModel: "requested", MappingJSON: `{"requested":"provider/model"}`,
			Success: false, StatusCode: 500,
		})
	}
	assert.Equal(t, 2, m.ConsecutiveFailures)
	assert.Equal(t, model.RouteHealthy, m.State())
	assert.Zero(t, requested.ConsecutiveFailures)
}

func TestApplyProductionOutcomeDisabledWhenChannelPriority(t *testing.T) {
	clearRouteTables(t)
	SetRoutingPriorityMode(model.RoutingPriorityModeChannel)
	ApplyProductionOutcome(ProductionOutcome{ChannelID: 1, RequestedModel: "x", Success: true})
	assert.Nil(t, GlobalMetricsRuntime.Get(MakeMetricsKey(1, "x")))
}

func TestScheduleShadowWithoutCaptureSkipsPing(t *testing.T) {
	clearRouteTables(t)
	SetRoutingPriorityMode(model.RoutingPriorityModeModel)
	ClearShadowCaptures()
	// empty capture must not dispatch
	ScheduleShadowProbeAfterProduction(nil, 1, "m")
	// capture without user messages must not dispatch
	ScheduleShadowProbeAfterProduction(&ProductionShadowCapture{
		OriginModel: "m",
		View:        ProductionRequestView{Messages: nil},
	}, 1, "m")
}
