package model

import "github.com/QuantumNous/new-api/common"

// ChannelDisplayInfo is the current, non-persistent channel data safe to add
// to an administrator-facing log response.
type ChannelDisplayInfo struct {
	Name    string
	BaseURL string
}

// GetChannelDisplayInfos resolves every positive channel ID at most once. It
// prefers the in-memory channel cache and performs one bulk database query for
// any cache misses, allowing deleted channels to degrade to missing entries.
func GetChannelDisplayInfos(channelIDs []int) (map[int]ChannelDisplayInfo, error) {
	infos := make(map[int]ChannelDisplayInfo)
	uniqueIDs := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID > 0 {
			uniqueIDs[channelID] = struct{}{}
		}
	}

	if len(uniqueIDs) == 0 {
		return infos, nil
	}

	missingIDs := make([]int, 0, len(uniqueIDs))
	if common.MemoryCacheEnabled {
		for channelID := range uniqueIDs {
			channel, err := CacheGetChannel(channelID)
			if err == nil && channel != nil {
				infos[channelID] = ChannelDisplayInfo{
					Name:    channel.Name,
					BaseURL: channel.GetBaseURL(),
				}
				continue
			}
			missingIDs = append(missingIDs, channelID)
		}
	} else {
		for channelID := range uniqueIDs {
			missingIDs = append(missingIDs, channelID)
		}
	}

	if len(missingIDs) == 0 {
		return infos, nil
	}

	channels, err := GetChannelsByIds(missingIDs)
	if err != nil {
		return infos, err
	}
	for _, channel := range channels {
		infos[channel.Id] = ChannelDisplayInfo{
			Name:    channel.Name,
			BaseURL: channel.GetBaseURL(),
		}
	}
	return infos, nil
}
