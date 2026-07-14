package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedChannelRelations(t *testing.T, id int, status int) {
	t.Helper()
	channel := &Channel{Id: id, Name: "channel", Models: "model", Status: status}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "model", ChannelId: id, Enabled: true,
	}).Error)
	require.NoError(t, UpsertChannelModelPolicy(&ChannelModelPolicy{
		ChannelID: int64(id), RequestedModel: "model", Enabled: true, Source: PolicySourceConfigured,
	}))
	require.NoError(t, UpsertChannelModelMetrics(&ChannelModelMetrics{
		ChannelID: int64(id), EffectiveModel: "model", RouteState: string(RouteUnknown),
	}))
}

func assertChannelRelationCounts(t *testing.T, id int, expected int64) {
	t.Helper()
	checks := []struct {
		model any
		where string
	}{
		{&Channel{}, "id = ?"},
		{&Ability{}, "channel_id = ?"},
		{&ChannelModelPolicy{}, "channel_id = ?"},
		{&ChannelModelMetrics{}, "channel_id = ?"},
	}
	for _, check := range checks {
		var count int64
		require.NoError(t, DB.Model(check.model).Where(check.where, id).Count(&count).Error)
		assert.Equal(t, expected, count)
	}
}

func TestChannelDeleteRemovesRelations(t *testing.T) {
	truncateTables(t)
	seedChannelRelations(t, 201, common.ChannelStatusEnabled)
	seedChannelRelations(t, 202, common.ChannelStatusEnabled)

	require.NoError(t, (&Channel{Id: 201}).Delete())
	assertChannelRelationCounts(t, 201, 0)
	assertChannelRelationCounts(t, 202, 1)
}

func TestBatchDeleteChannelsRemovesRelations(t *testing.T) {
	truncateTables(t)
	seedChannelRelations(t, 211, common.ChannelStatusEnabled)
	seedChannelRelations(t, 212, common.ChannelStatusEnabled)
	seedChannelRelations(t, 213, common.ChannelStatusEnabled)

	require.NoError(t, BatchDeleteChannels([]int{211, 212}))
	assertChannelRelationCounts(t, 211, 0)
	assertChannelRelationCounts(t, 212, 0)
	assertChannelRelationCounts(t, 213, 1)
}

func TestDeleteDisabledChannelRemovesOnlyDisabledRelations(t *testing.T) {
	truncateTables(t)
	seedChannelRelations(t, 221, common.ChannelStatusEnabled)
	seedChannelRelations(t, 222, common.ChannelStatusAutoDisabled)
	seedChannelRelations(t, 223, common.ChannelStatusManuallyDisabled)

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	assertChannelRelationCounts(t, 221, 1)
	assertChannelRelationCounts(t, 222, 0)
	assertChannelRelationCounts(t, 223, 0)
}

func TestBatchDeleteChannelsRollsBackRelationFailure(t *testing.T) {
	truncateTables(t)
	seedChannelRelations(t, 231, common.ChannelStatusEnabled)

	callbackName := "test:channel-delete-relation-failure"
	require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "channel_model_metrics" {
			tx.AddError(errors.New("injected metrics delete failure"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Delete().Remove(callbackName))
	})

	require.Error(t, BatchDeleteChannels([]int{231}))
	assertChannelRelationCounts(t, 231, 1)
}
