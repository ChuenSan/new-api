package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelDisplayPageResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []map[string]json.RawMessage `json:"items"`
	} `json:"data"`
}

func setupChannelDisplayControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCache := common.MemoryCacheEnabled
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Log{}, &model.Midjourney{}, &model.Task{}, &model.User{}))
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCache
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performChannelDisplayPageRequest(t *testing.T, handler gin.HandlerFunc, path string, userID int) channelDisplayPageResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path+"?p=1&page_size=10", nil)
	if userID != 0 {
		ctx.Set("id", userID)
	}
	handler(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelDisplayPageResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	return response
}

func assertChannelBaseURL(t *testing.T, item map[string]json.RawMessage, expected string) {
	t.Helper()
	var value string
	require.NoError(t, json.Unmarshal(item["channel_base_url"], &value))
	assert.Equal(t, expected, value)
}

func assertChannelBaseURLOmitted(t *testing.T, item map[string]json.RawMessage) {
	t.Helper()
	assert.NotContains(t, item, "channel_base_url")
}

// TestAdministratorLogListsExposeChannelBaseURLOnlyToAdministrators guards the
// three list contracts and ensures self-service responses omit the new field.
func TestAdministratorLogListsExposeChannelBaseURLOnlyToAdministrators(t *testing.T) {
	db := setupChannelDisplayControllerTestDB(t)
	baseURL := "api.channel.example/v1"
	require.NoError(t, db.Create(&model.Channel{Id: 801, Name: "test channel", BaseURL: &baseURL}).Error)
	require.NoError(t, db.Create(&model.User{Id: 71, Username: "test-user"}).Error)
	require.NoError(t, db.Create(&model.Log{Id: 1, UserId: 71, ChannelId: 801, CreatedAt: 100}).Error)
	require.NoError(t, db.Create(&model.Midjourney{Id: 1, UserId: 71, ChannelId: 801, SubmitTime: 100}).Error)
	require.NoError(t, db.Create(&model.Task{ID: 1, UserId: 71, ChannelId: 801, SubmitTime: 100}).Error)

	assertChannelBaseURL(t, performChannelDisplayPageRequest(t, GetAllLogs, "/api/log", 0).Data.Items[0], baseURL)
	assertChannelBaseURLOmitted(t, performChannelDisplayPageRequest(t, GetUserLogs, "/api/log/self", 71).Data.Items[0])

	assertChannelBaseURL(t, performChannelDisplayPageRequest(t, GetAllMidjourney, "/api/mj", 0).Data.Items[0], baseURL)
	assertChannelBaseURLOmitted(t, performChannelDisplayPageRequest(t, GetUserMidjourney, "/api/mj/self", 71).Data.Items[0])

	assertChannelBaseURL(t, performChannelDisplayPageRequest(t, GetAllTask, "/api/task", 0).Data.Items[0], baseURL)
	assertChannelBaseURLOmitted(t, performChannelDisplayPageRequest(t, GetUserTask, "/api/task/self", 71).Data.Items[0])
}
