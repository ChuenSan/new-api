package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPendingLogTestContext(requestId string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, requestId)
	c.Set("username", "test-user")
	return c
}

func TestPendingRequestLogSettlesIntoConsumeLog(t *testing.T) {
	truncateTables(t)
	c := newPendingLogTestContext("pending-consume-request")

	RecordRequestStartLog(c, RecordRequestStartLogParams{
		UserId:    42,
		RequestId: "pending-consume-request",
		ModelName: "gpt-test",
		TokenName: "test-token",
		TokenId:   7,
		Group:     "default",
		IsStream:  true,
	})
	RecordConsumeLog(c, 42, RecordConsumeLogParams{
		ChannelId:        3,
		PromptTokens:     12,
		CompletionTokens: 8,
		ModelName:        "gpt-test",
		TokenName:        "test-token",
		Quota:            120,
		Content:          "completed",
		TokenId:          7,
		UseTimeSeconds:   2,
		IsStream:         true,
		Group:            "default",
	})

	var logs []*Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "pending-consume-request").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, LogStatusCompleted, logs[0].Status)
	assert.Equal(t, LogTypeConsume, logs[0].Type)
	assert.Equal(t, 120, logs[0].Quota)
	assert.Equal(t, 3, logs[0].ChannelId)
}

func TestFinalizePendingRequestCreatesFailedLog(t *testing.T) {
	truncateTables(t)
	c := newPendingLogTestContext("pending-failed-request")

	RecordRequestStartLog(c, RecordRequestStartLogParams{
		UserId:    42,
		RequestId: "pending-failed-request",
		ModelName: "gpt-test",
	})
	FinalizePendingRequest(c, FinalizePendingRequestParams{
		UserId:         42,
		ModelName:      "gpt-test",
		Content:        "upstream timeout",
		UseTimeSeconds: 5,
	})

	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "pending-failed-request").First(&log).Error)
	assert.Equal(t, LogStatusFailed, log.Status)
	assert.Equal(t, LogTypeError, log.Type)
	assert.Equal(t, "upstream timeout", log.Content)
}
