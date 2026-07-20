package modelroute

import "github.com/QuantumNous/new-api/model"

func ResetRouteToUnknown(channelID int64, effectiveModel string, requestedModels []string) error {
	mk := MakeMetricsKey(channelID, effectiveModel)
	lock := metricsLockFor(mk)
	lock.Lock()
	defer lock.Unlock()

	var runtime *model.ChannelModelMetrics
	if current := GlobalMetricsRuntime.Get(mk); current != nil {
		copy := *current
		runtime = &copy
	}
	if _, err := model.ResetChannelModelMetricsUnknown(channelID, effectiveModel, runtime); err != nil {
		return err
	}

	GlobalMetricsRuntime.Delete(mk)
	GlobalCalibrationPersister.ClearDirty(mk)
	GlobalRoles.Set(mk, model.RoleNone)
	seen := make(map[string]struct{}, len(requestedModels))
	for _, requestedModel := range requestedModels {
		if requestedModel == "" {
			continue
		}
		if _, ok := seen[requestedModel]; ok {
			continue
		}
		seen[requestedModel] = struct{}{}
		InvalidateRoutePlan(requestedModel)
		GlobalLeases.ClearLeaseIfMetricsKey(requestedModel, mk)
	}
	return nil
}
