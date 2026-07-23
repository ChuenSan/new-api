package modelroute

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// MigrationResult summarizes one-click model-priority migration (PRD §4).
type MigrationResult struct {
	PoliciesTouched int               `json:"policies_touched"`
	MetricsTouched  int               `json:"metrics_touched"`
	PoliciesSeeded  int               `json:"policies_seeded"`
	PoliciesPruned  int               `json:"policies_pruned"`
	MetricsPruned   int               `json:"metrics_pruned"`
	ChannelsZeroed  int               `json:"channels_zeroed"`
	Mode            string            `json:"mode"`
	Backup          []ChannelPWBackup `json:"backup,omitempty"`
}

// ChannelPWBackup is pre-migration priority/weight for audit (PRD §4 step 1).
type ChannelPWBackup struct {
	ChannelID int   `json:"channel_id"`
	Priority  int64 `json:"priority"`
	Weight    int   `json:"weight"`
}

// MigrateToModelPriority runs migration steps 1–9 (PRD §4). Step 10 audit is caller's responsibility.
func MigrateToModelPriority() (*MigrationResult, error) {
	channels, err := model.GetAllChannels(0, 0, true, true)
	if err != nil {
		return nil, err
	}
	res := &MigrationResult{}
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		res.Backup = append(res.Backup, ChannelPWBackup{
			ChannelID: ch.Id,
			Priority:  ch.GetPriority(),
			Weight:    ch.GetWeight(),
		})
	}

	// steps 2–5: discover + create/seed policy/metrics (must run before channel priority zeroing)
	var allPairs []DiscoveredModelPair
	for _, ch := range channels {
		allPairs = append(allPairs, DiscoverFromChannel(ch)...)
	}
	pCount, mCount, seeded, err := MaterializeDiscovery(allPairs)
	if err != nil {
		return nil, err
	}
	res.PoliciesTouched = pCount
	res.MetricsTouched = mCount
	res.PoliciesSeeded = seeded

	// prune configured/mapped policies no longer declared by channel models/mapping
	pruneRes, err := PruneOrphanPoliciesAll(PruneOptions{})
	if err != nil {
		return nil, err
	}
	res.PoliciesPruned = pruneRes.PoliciesDeleted
	res.MetricsPruned = pruneRes.MetricsDeleted

	// steps 6–7: zero channel priority/weight after model policies absorbed them
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		if ch.GetPriority() == 0 && ch.GetWeight() == 0 {
			continue
		}
		if err := model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).
			Updates(map[string]interface{}{
				"priority": int64(0),
				"weight":   uint(0),
			}).Error; err != nil {
			return nil, fmt.Errorf("zero channel %d: %w", ch.Id, err)
		}
		res.ChannelsZeroed++
	}

	// step 8: switch mode (persist first; in-memory only after DB ok)
	if err := model.UpdateOption(model.RoutingPriorityModeKey, model.RoutingPriorityModeModel); err != nil {
		return nil, err
	}
	SetRoutingPriorityMode(model.RoutingPriorityModeModel)
	res.Mode = model.RoutingPriorityModeModel

	// step 9: refresh caches
	InvalidateAllRoutePlans()
	if common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	// Probe queue only meaningful under model_priority; reconcile after mode switch.
	if err := ReconcileProbeQueueFromDB(); err != nil {
		return nil, fmt.Errorf("reconcile probe queue: %w", err)
	}
	return res, nil
}

// ResetRuntimeLearning clears short-term metrics for one or all routes (PRD §18.1).
// When channelID==0 and effectiveModel=="", resets all rows.
func ResetRuntimeLearning(channelID int64, effectiveModel string) (int, error) {
	if channelID > 0 && effectiveModel != "" {
		if err := model.ResetChannelModelMetricsRuntime(channelID, effectiveModel); err != nil {
			return 0, err
		}
		mk := MakeMetricsKey(channelID, effectiveModel)
		GlobalMetricsRuntime.mu.Lock()
		delete(GlobalMetricsRuntime.data, mk.String())
		GlobalMetricsRuntime.mu.Unlock()
		return 1, nil
	}
	rows, err := model.ListAllChannelModelMetrics()
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range rows {
		if err := model.ResetChannelModelMetricsRuntime(rows[i].ChannelID, rows[i].EffectiveModel); err != nil {
			return n, err
		}
		n++
	}
	GlobalMetricsRuntime.Clear()
	return n, nil
}

// ResetAllLearning clears short-term metrics and calibration (PRD §18.2).
func ResetAllLearning(channelID int64, effectiveModel string) (int, error) {
	if channelID > 0 && effectiveModel != "" {
		if err := model.ResetChannelModelMetricsAll(channelID, effectiveModel); err != nil {
			return 0, err
		}
		mk := MakeMetricsKey(channelID, effectiveModel)
		GlobalMetricsRuntime.mu.Lock()
		delete(GlobalMetricsRuntime.data, mk.String())
		GlobalMetricsRuntime.mu.Unlock()
		GlobalShadowTransport.Reset(mk.String())
		return 1, nil
	}
	rows, err := model.ListAllChannelModelMetrics()
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range rows {
		if err := model.ResetChannelModelMetricsAll(rows[i].ChannelID, rows[i].EffectiveModel); err != nil {
			return n, err
		}
		n++
	}
	GlobalMetricsRuntime.Clear()
	GlobalShadowTransport.Clear()
	return n, nil
}

// AdminForceState applies admin ops: trip open / force probe / manual disable / restore (PRD §34).
func AdminForceState(channelID int64, effectiveModel string, event TransitionEvent) error {
	m, err := LoadOrEnsureMetrics(channelID, effectiveModel)
	if err != nil {
		return err
	}
	ApplyTransition(m, event, 0)
	return GlobalCalibrationPersister.SnapshotCritical(m)
}

// CascadeMetricsForChannelStatus applies metrics transitions when a channel is
// manually enabled/disabled. status=2 → EventManualDisable for all metrics;
// status=1 → EventRestoreAuto; other status → no-op. Empty metrics list is OK.
func CascadeMetricsForChannelStatus(channelID int64, status int) (int, error) {
	var event TransitionEvent
	switch status {
	case common.ChannelStatusManuallyDisabled:
		event = EventManualDisable
	case common.ChannelStatusEnabled:
		event = EventRestoreAuto
	default:
		return 0, nil
	}
	rows, err := model.ListChannelModelMetricsByChannel(channelID)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	n := 0
	var firstErr error
	for i := range rows {
		if err := AdminForceState(rows[i].ChannelID, rows[i].EffectiveModel, event); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cascade metrics channel=%d model=%s: %w", rows[i].ChannelID, rows[i].EffectiveModel, err)
			}
			// channel status already committed; keep cascading remaining rows
			continue
		}
		n++
	}
	return n, firstErr
}
