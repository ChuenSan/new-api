package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertClaudeRequestSetsDeepConversionOnlyForChat verifies the R0 target-protocol guard.
func TestConvertClaudeRequestSetsDeepConversionOnlyForChat(t *testing.T) {
	tests := []struct {
		name        string
		relayMode   int
		channelType int
		relayFormat types.RelayFormat
		want        bool
	}{
		{name: "OpenAI chat completions", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeOpenAI, relayFormat: types.RelayFormatClaude, want: true},
		{name: "Azure chat completions", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeAzure, relayFormat: types.RelayFormatClaude, want: true},
		{name: "OpenRouter chat completions", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeOpenRouter, relayFormat: types.RelayFormatClaude, want: true},
		{name: "Responses", relayMode: relayconstant.RelayModeResponses, channelType: constant.ChannelTypeOpenAI, relayFormat: types.RelayFormatClaude, want: false},
		{name: "Responses compact", relayMode: relayconstant.RelayModeResponsesCompact, channelType: constant.ChannelTypeOpenAI, relayFormat: types.RelayFormatClaude, want: false},
		{name: "Gemini input", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeOpenAI, relayFormat: types.RelayFormatGemini, want: false},
		{name: "native Messages channel", relayMode: relayconstant.RelayModeChatCompletions, channelType: constant.ChannelTypeAnthropic, relayFormat: types.RelayFormatClaude, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: tt.relayFormat,
				RelayMode:   tt.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tt.channelType},
			}

			_, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, &dto.ClaudeRequest{Model: "test-model"})
			require.NoError(t, err)
			assert.Equal(t, tt.want, info.AnthropicMessagesToOpenAIChatCompletions)
		})
	}
}
