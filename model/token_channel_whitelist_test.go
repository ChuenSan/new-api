package model

import (
	"database/sql"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestChannelIDListPreservesNullAndArray guards the persistence contract for unrestricted and restricted keys.
func TestChannelIDListPreservesNullAndArray(t *testing.T) {
	var nilList ChannelIDList
	value, err := nilList.Value()
	require.NoError(t, err)
	require.Nil(t, value)

	original := ChannelIDList{3, 7}
	value, err = original.Value()
	require.NoError(t, err)

	var decoded ChannelIDList
	require.NoError(t, decoded.Scan(value))
	require.Equal(t, original, decoded)
}

// TestChannelIDListDatabaseRoundTrip guards SQL NULL, JSON array, and update-to-NULL behavior.
func TestChannelIDListDatabaseRoundTrip(t *testing.T) {
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	previousDB := DB
	previousLogDB := LOG_DB
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&Token{}))

	unrestricted := Token{UserId: 1, Key: "unrestricted-key", Name: "unrestricted"}
	restricted := Token{UserId: 1, Key: "restricted-key", Name: "restricted", AllowedChannelIds: ChannelIDList{3, 7}}
	require.NoError(t, db.Create(&unrestricted).Error)
	require.NoError(t, db.Create(&restricted).Error)

	var unrestrictedRaw sql.NullString
	require.NoError(t, db.Raw("SELECT allowed_channel_ids FROM tokens WHERE id = ?", unrestricted.Id).Row().Scan(&unrestrictedRaw))
	require.False(t, unrestrictedRaw.Valid)

	var loaded Token
	require.NoError(t, db.First(&loaded, restricted.Id).Error)
	require.Equal(t, ChannelIDList{3, 7}, loaded.AllowedChannelIds)

	loaded.AllowedChannelIds = nil
	require.NoError(t, loaded.Update())
	require.NoError(t, db.Raw("SELECT allowed_channel_ids FROM tokens WHERE id = ?", restricted.Id).Row().Scan(&unrestrictedRaw))
	require.False(t, unrestrictedRaw.Valid)
}

// TestGetRandomSatisfiedChannelFiltersBeforePrioritySelection guards whitelist filtering before legacy priority routing.
func TestGetRandomSatisfiedChannelFiltersBeforePrioritySelection(t *testing.T) {
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	previousMemoryCache := common.MemoryCacheEnabled
	previousDB := DB
	previousLogDB := LOG_DB
	common.MemoryCacheEnabled = false

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		DB = previousDB
		LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

	highPriority := int64(100)
	lowPriority := int64(10)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "high", Key: "key-1", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{Id: 2, Name: "allowed", Key: "key-2", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "gpt-test", ChannelId: 1, Enabled: true, Priority: &highPriority, Weight: 1}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "gpt-test", ChannelId: 2, Enabled: true, Priority: &lowPriority, Weight: 1}).Error)

	channel, err := GetRandomSatisfiedChannel("default", "gpt-test", 0, "", map[int]struct{}{2: {}})
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 2, channel.Id)

	channel, err = GetRandomSatisfiedChannel("default", "gpt-test", 0, "", map[int]struct{}{})
	require.NoError(t, err)
	require.Nil(t, channel)
}
