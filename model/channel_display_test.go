package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestGetChannelDisplayInfosBatchesUniqueIDs verifies that current display
// data is resolved once per channel and deleted channels safely disappear.
func TestGetChannelDisplayInfosBatchesUniqueIDs(t *testing.T) {
	truncateTables(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
	})

	firstURL := "api.first.example/v1"
	secondURL := "https://api.second.example/v1"
	require.NoError(t, DB.Create(&Channel{Id: 501, Name: "first", BaseURL: &firstURL}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 502, Name: "second", BaseURL: &secondURL}).Error)

	queryCount := 0
	callbackName := "test:channel-display-batch"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			queryCount++
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})

	infos, err := GetChannelDisplayInfos([]int{0, -1, 501, 502, 501, 999})
	require.NoError(t, err)
	assert.Equal(t, 1, queryCount)
	assert.Equal(t, ChannelDisplayInfo{Name: "first", BaseURL: firstURL}, infos[501])
	assert.Equal(t, ChannelDisplayInfo{Name: "second", BaseURL: secondURL}, infos[502])
	assert.NotContains(t, infos, 999)
}
