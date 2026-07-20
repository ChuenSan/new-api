package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/modelroute"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type modelRouteMutationResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		RequestedModel string                            `json:"requested_model"`
		Changed        []model.ModelPolicyPriorityChange `json:"changed"`
		Policies       []modelRoutePolicyView            `json:"policies"`
	} `json:"data"`
}

type modelRoutePolicyListResponse struct {
	Success bool                   `json:"success"`
	Data    []modelRoutePolicyView `json:"data"`
}

func setupModelRouteControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelModelPolicy{},
		&model.ChannelModelMetrics{},
		&model.User{},
		&model.Log{},
	))
	modelroute.GlobalMetricsRuntime.Clear()
	modelroute.GlobalRoles.Clear()
	modelroute.GlobalLeases.ClearAll()
	modelroute.InvalidateAllRoutePlans()
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performModelRouteMutation(t *testing.T, handler gin.HandlerFunc, body map[string]interface{}) (*httptest.ResponseRecorder, modelRouteMutationResponse) {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/model_route/policies", bytes.NewReader(payload))
	handler(ctx)

	var response modelRouteMutationResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

type metricsActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func performMetricsAction(t *testing.T, body map[string]interface{}) (*httptest.ResponseRecorder, metricsActionResponse) {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 7)
	ctx.Set("username", "root")
	ctx.Set("role", 100)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/model_route/metrics/action", bytes.NewReader(payload))
	ModelRouteMetricsAction(ctx)

	var response metricsActionResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func seedControllerModelPolicies(t *testing.T, requestedModel string, priorities map[int64]int) {
	t.Helper()
	policies := make([]model.ChannelModelPolicy, 0, len(priorities))
	for channelID, priority := range priorities {
		policies = append(policies, model.ChannelModelPolicy{
			ChannelID: channelID, RequestedModel: requestedModel, ManualPriority: priority,
			Enabled: true, Source: model.PolicySourceConfigured,
		})
	}
	require.NoError(t, model.UpsertChannelModelPolicies(policies))
}

func performModelRoutePolicyList(t *testing.T) (*httptest.ResponseRecorder, modelRoutePolicyListResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/model_route/policies", nil)
	ListModelRoutePolicies(ctx)

	var response modelRoutePolicyListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func seedControllerChannel(t *testing.T, id int, name string, status int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: name, Status: status}).Error)
}

func TestListModelRoutePoliciesIncludesChannelStatus(t *testing.T) {
	setupModelRouteControllerTestDB(t)
	seedControllerChannel(t, 1, "enabled", common.ChannelStatusEnabled)
	seedControllerChannel(t, 2, "manual", common.ChannelStatusManuallyDisabled)
	seedControllerChannel(t, 3, "auto", common.ChannelStatusAutoDisabled)
	seedControllerModelPolicies(t, "gpt-status", map[int64]int{1: 100, 2: 90, 3: 80, 4: 70})

	recorder, response := performModelRoutePolicyList(t)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, response.Success)
	require.Len(t, response.Data, 4)
	byID := make(map[int64]modelRoutePolicyView, len(response.Data))
	for _, policy := range response.Data {
		byID[policy.ChannelID] = policy
	}
	assert.Equal(t, common.ChannelStatusEnabled, byID[1].ChannelStatus)
	assert.True(t, byID[1].ChannelExists)
	assert.Equal(t, "enabled", byID[1].ChannelName)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, byID[2].ChannelStatus)
	assert.True(t, byID[2].ChannelExists)
	assert.Equal(t, common.ChannelStatusAutoDisabled, byID[3].ChannelStatus)
	assert.True(t, byID[3].ChannelExists)
	assert.Equal(t, 0, byID[4].ChannelStatus)
	assert.False(t, byID[4].ChannelExists)
}

func TestModelRouteMetricsActionResetUnknown(t *testing.T) {
	db := setupModelRouteControllerTestDB(t)
	mapping := `{"request-a":"effective","request-b":"effective"}`
	require.NoError(t, db.Create(&model.Channel{
		Id: 51, Name: "mapped", Status: common.ChannelStatusEnabled, ModelMapping: &mapping,
	}).Error)
	seedControllerModelPolicies(t, "request-a", map[int64]int{51: 100})
	seedControllerModelPolicies(t, "request-b", map[int64]int{51: 90})
	cooldown := int64(1_700_000_000)
	require.NoError(t, model.UpsertChannelModelMetrics(&model.ChannelModelMetrics{
		ChannelID: 51, EffectiveModel: "effective", RouteState: string(model.RouteOpen),
		BackoffLevel: 2, CooldownUntil: &cooldown, LastErrorClass: string(model.ErrorDeterministic),
	}))
	modelroute.StoreRoutePlan(&model.RoutePlan{RequestedModel: "request-a"})
	modelroute.StoreRoutePlan(&model.RoutePlan{RequestedModel: "request-b"})

	recorder, response := performMetricsAction(t, map[string]interface{}{
		"channel_id": 51, "effective_model": "  effective  ", "action": "reset_unknown",
	})
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, response.Success)
	stored, err := model.GetChannelModelMetrics(51, "effective")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.RouteUnknown, stored.State())
	assert.Zero(t, stored.BackoffLevel)
	assert.Nil(t, stored.CooldownUntil)
	assert.Empty(t, stored.LastErrorClass)
	assert.Nil(t, modelroute.GetCachedRoutePlan("request-a"))
	assert.Nil(t, modelroute.GetCachedRoutePlan("request-b"))
	var audit model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).First(&audit).Error)
	assert.Equal(t, 7, audit.UserId)
	assert.Positive(t, audit.CreatedAt)
	var auditData struct {
		Op struct {
			Action string `json:"action"`
			Params struct {
				ChannelID      int64  `json:"channel_id"`
				EffectiveModel string `json:"effective_model"`
				Action         string `json:"action"`
			} `json:"params"`
		} `json:"op"`
		AdminInfo struct {
			AdminID int `json:"admin_id"`
		} `json:"admin_info"`
	}
	require.NoError(t, common.Unmarshal([]byte(audit.Other), &auditData))
	assert.Equal(t, "model_route.metrics_action", auditData.Op.Action)
	assert.Equal(t, int64(51), auditData.Op.Params.ChannelID)
	assert.Equal(t, "effective", auditData.Op.Params.EffectiveModel)
	assert.Equal(t, "reset_unknown", auditData.Op.Params.Action)
	assert.Equal(t, 7, auditData.AdminInfo.AdminID)
}

func TestModelRouteMetricsActionResetUnknownRejectsInvalidOrMissingTarget(t *testing.T) {
	setupModelRouteControllerTestDB(t)
	tests := []struct {
		name string
		body map[string]interface{}
		code int
	}{
		{name: "negative channel", body: map[string]interface{}{
			"channel_id": -1, "effective_model": "m", "action": "reset_unknown",
		}, code: http.StatusBadRequest},
		{name: "blank model", body: map[string]interface{}{
			"channel_id": 1, "effective_model": "   ", "action": "reset_unknown",
		}, code: http.StatusBadRequest},
		{name: "missing row", body: map[string]interface{}{
			"channel_id": 404, "effective_model": "missing", "action": "reset_unknown",
		}, code: http.StatusOK},
		{name: "unknown action", body: map[string]interface{}{
			"channel_id": 1, "effective_model": "m", "action": "unknown",
		}, code: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, response := performMetricsAction(t, test.body)
			assert.Equal(t, test.code, recorder.Code)
			assert.False(t, response.Success)
		})
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.ChannelModelMetrics{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateModelRoutePolicyPrioritySwapsAtomically(t *testing.T) {
	db := setupModelRouteControllerTestDB(t)
	seedControllerModelPolicies(t, "gpt-priority", map[int64]int{1: 100, 2: 90})

	recorder, response := performModelRouteMutation(t, UpdateModelRoutePolicyPriority, map[string]interface{}{
		"channel_id": 2, "requested_model": "gpt-priority", "manual_priority": 100,
		"expected_manual_priority": 90, "conflict_strategy": "swap",
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, response.Success)
	assert.ElementsMatch(t, []model.ModelPolicyPriorityChange{
		{ChannelID: 2, ManualPriority: 100},
		{ChannelID: 1, ManualPriority: 90},
	}, response.Data.Changed)
	require.Len(t, response.Data.Policies, 2)
	var auditCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&auditCount).Error)
	assert.Equal(t, int64(1), auditCount)
}

func TestUpdateModelRoutePolicyPriorityRejectsStaleSnapshot(t *testing.T) {
	setupModelRouteControllerTestDB(t)
	seedControllerModelPolicies(t, "gpt-priority", map[int64]int{1: 100, 2: 80})

	recorder, response := performModelRouteMutation(t, UpdateModelRoutePolicyPriority, map[string]interface{}{
		"channel_id": 2, "requested_model": "gpt-priority", "manual_priority": 100,
		"expected_manual_priority": 90, "conflict_strategy": "swap",
	})

	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.False(t, response.Success)
	assert.Equal(t, "stale_policy_snapshot", response.Code)
}

func TestReorderModelRoutePoliciesValidatesCompleteModelGroup(t *testing.T) {
	setupModelRouteControllerTestDB(t)
	seedControllerModelPolicies(t, "gpt-priority", map[int64]int{1: 100, 2: 90})
	seedControllerModelPolicies(t, "other-model", map[int64]int{3: 80})

	tests := []struct {
		name     string
		ordered  []int64
		expected []map[string]interface{}
	}{
		{
			name:    "duplicate id",
			ordered: []int64{1, 1},
			expected: []map[string]interface{}{
				{"channel_id": 1, "manual_priority": 100},
				{"channel_id": 2, "manual_priority": 90},
			},
		},
		{
			name:    "incomplete group",
			ordered: []int64{1},
			expected: []map[string]interface{}{
				{"channel_id": 1, "manual_priority": 100},
			},
		},
		{
			name:    "policy from another model",
			ordered: []int64{1, 3},
			expected: []map[string]interface{}{
				{"channel_id": 1, "manual_priority": 100},
				{"channel_id": 3, "manual_priority": 80},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, response := performModelRouteMutation(t, ReorderModelRoutePolicies, map[string]interface{}{
				"requested_model": "gpt-priority", "ordered_channel_ids": test.ordered,
				"expected": test.expected, "moved_channel_id": test.ordered[0],
			})
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Equal(t, "invalid_order", response.Code)
		})
	}
}

func TestReorderModelRoutePoliciesReturnsAuthoritativeGroup(t *testing.T) {
	setupModelRouteControllerTestDB(t)
	seedControllerModelPolicies(t, "gpt-priority", map[int64]int{1: 100, 2: 90, 3: 0})

	recorder, response := performModelRouteMutation(t, ReorderModelRoutePolicies, map[string]interface{}{
		"requested_model": "gpt-priority", "ordered_channel_ids": []int64{1, 3, 2},
		"moved_channel_id": 3,
		"expected": []map[string]interface{}{
			{"channel_id": 1, "manual_priority": 100},
			{"channel_id": 2, "manual_priority": 90},
			{"channel_id": 3, "manual_priority": 0},
		},
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, response.Success)
	assert.Equal(t, "gpt-priority", response.Data.RequestedModel)
	assert.Equal(t, []model.ModelPolicyPriorityChange{{ChannelID: 3, ManualPriority: 95}}, response.Data.Changed)
	require.Len(t, response.Data.Policies, 3)
	assert.Equal(t, []int64{1, 3, 2}, []int64{
		response.Data.Policies[0].ChannelID,
		response.Data.Policies[1].ChannelID,
		response.Data.Policies[2].ChannelID,
	})
}
