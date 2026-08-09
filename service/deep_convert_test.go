package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func strictChatRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{AnthropicMessagesToOpenAIChatCompletions: true}
}

// TestClaudeToOpenAIChatCompletionsRequest preserves mixed assistant content and tool calls.
func TestClaudeToOpenAIChatCompletionsRequest(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "gpt-test",
		System: []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "text", "text": "second"},
		},
		Tools: []any{map[string]any{
			"name": "lookup",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]any{"type": "string", "format": "uri"}},
				"$defs":      map[string]any{"x": map[string]any{"type": "string"}},
			},
		}},
		ToolChoice: map[string]any{"type": "any", "disable_parallel_tool_use": true},
		Messages: []dto.ClaudeMessage{{Role: "assistant", Content: []any{
			map[string]any{"type": "text", "text": "checking"},
			map[string]any{"type": "tool_use", "id": "call_1", "name": "lookup", "input": map[string]any{"q": "x"}},
		}}},
	}

	converted, err := ClaudeToOpenAIRequest(request, strictChatRelayInfo())
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	require.Equal(t, "first\nsecond", converted.Messages[0].StringContent())
	require.Equal(t, "checking", converted.Messages[1].StringContent())
	require.Len(t, converted.Messages[1].ParseToolCalls(), 1)
	require.Equal(t, "required", converted.ToolChoice)
	require.NotNil(t, converted.ParallelTooCalls)
	require.False(t, *converted.ParallelTooCalls)
	require.NotContains(t, converted.Tools[0].Function.Parameters.(map[string]any)["properties"].(map[string]any)["url"].(map[string]any), "format")
}

// TestClaudeToOpenAIChatCompletionsToolChoicePresence preserves omission versus explicit false.
func TestClaudeToOpenAIChatCompletionsToolChoicePresence(t *testing.T) {
	tools := []any{map[string]any{"name": "lookup", "input_schema": map[string]any{"type": "object"}}}
	tests := []struct {
		name     string
		choice   map[string]any
		parallel *bool
	}{
		{name: "auto omitted", choice: map[string]any{"type": "auto"}},
		{name: "any explicit false", choice: map[string]any{"type": "any", "disable_parallel_tool_use": false}, parallel: boolPointer(true)},
		{name: "none explicit true", choice: map[string]any{"type": "none", "disable_parallel_tool_use": true}, parallel: boolPointer(false)},
		{name: "tool explicit false", choice: map[string]any{"type": "tool", "name": "lookup", "disable_parallel_tool_use": false}, parallel: boolPointer(true)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{Tools: tools, ToolChoice: tt.choice}, strictChatRelayInfo())
			require.NoError(t, err)
			if tt.parallel == nil {
				require.Nil(t, converted.ParallelTooCalls)
				return
			}
			require.NotNil(t, converted.ParallelTooCalls)
			require.Equal(t, *tt.parallel, *converted.ParallelTooCalls)
		})
	}
}

// TestClaudeToOpenAIChatCompletionsReasoningKeepsOnlyOpenRouterDetails protects standard Chat from non-standard fields.
func TestClaudeToOpenAIChatCompletionsReasoningKeepsOnlyOpenRouterDetails(t *testing.T) {
	request := dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "assistant", Content: []any{
		map[string]any{"type": "thinking", "thinking": "considering", "signature": "sig"},
		map[string]any{"type": "redacted_thinking", "data": "encrypted"},
		map[string]any{"type": "text", "text": "answer"},
	}}}}

	standard, err := ClaudeToOpenAIRequest(request, strictChatRelayInfo())
	require.NoError(t, err)
	require.Len(t, standard.Messages, 1)
	require.Empty(t, standard.Messages[0].ReasoningDetails)

	openRouterInfo := strictChatRelayInfo()
	openRouterInfo.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
	converted, err := ClaudeToOpenAIRequest(request, openRouterInfo)
	require.NoError(t, err)
	var details []map[string]any
	require.NoError(t, json.Unmarshal(converted.Messages[0].ReasoningDetails, &details))
	require.Equal(t, []map[string]any{
		{"type": "reasoning.text", "text": "considering", "signature": "sig", "format": "anthropic-claude-v1"},
		{"type": "reasoning.encrypted", "data": "encrypted", "format": "anthropic-claude-v1"},
	}, details)
}

// TestClaudeToOpenAIChatCompletionsMapsEffort preserves adapter-supported OpenAI reasoning effort.
func TestClaudeToOpenAIChatCompletionsMapsEffort(t *testing.T) {
	info := strictChatRelayInfo()
	converted, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{OutputConfig: json.RawMessage(`{"effort":"high"}`)}, info)
	require.NoError(t, err)
	require.Equal(t, "high", converted.ReasoningEffort)

	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
	converted, err = ClaudeToOpenAIRequest(dto.ClaudeRequest{OutputConfig: json.RawMessage(`{"effort":"low"}`)}, info)
	require.NoError(t, err)
	var reasoning map[string]any
	require.NoError(t, json.Unmarshal(converted.Reasoning, &reasoning))
	require.Equal(t, true, reasoning["enabled"])
	require.Equal(t, "low", reasoning["effort"])
}

// TestClaudeToOpenAIChatCompletionsRequest rejects invalid deep-conversion inputs.
func TestClaudeToOpenAIChatCompletionsRequestRejectsInvalidInput(t *testing.T) {
	_, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{
		Tools: []any{map[string]any{"name": ""}},
	}, strictChatRelayInfo())
	require.Error(t, err)

	_, err = ClaudeToOpenAIRequest(dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: []any{
		map[string]any{"type": "image", "source": nil},
	}}}}, strictChatRelayInfo())
	require.Error(t, err)
}

// TestResponseOpenAIChatCompletions2Claude uses the first choice in block order.
func TestResponseOpenAIChatCompletions2Claude(t *testing.T) {
	reasoning := "reason"
	refusal := "cannot do that"
	response := &dto.OpenAITextResponse{
		Id: "chatcmpl_1", Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{FinishReason: "tool_calls", Message: dto.Message{
				Content:          "answer",
				ReasoningContent: &reasoning,
				Refusal:          &refusal,
				ToolCalls:        []byte(`[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]`),
			}},
			{FinishReason: "stop", Message: dto.Message{Content: "must not be used"}},
		},
	}

	converted := ResponseOpenAI2Claude(response, strictChatRelayInfo())
	require.NotNil(t, converted)
	require.Equal(t, "tool_use", converted.StopReason)
	require.Len(t, converted.Content, 4)
	require.Equal(t, "thinking", converted.Content[0].Type)
	require.Equal(t, "text", converted.Content[1].Type)
	require.Equal(t, "answer", converted.Content[1].GetText())
	require.Equal(t, "cannot do that", converted.Content[2].GetText())
	require.Equal(t, "tool_use", converted.Content[3].Type)
	require.Equal(t, map[string]any{"q": "x"}, converted.Content[3].Input)
	require.NotNil(t, converted.Usage)
}

// TestResponseOpenAIChatCompletions2ClaudeReadsOutputTextAndLegacyFunctionCall preserves response compatibility.
func TestResponseOpenAIChatCompletions2ClaudeReadsOutputTextAndLegacyFunctionCall(t *testing.T) {
	response := &dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{
		FinishReason: "function_call",
		Message: dto.Message{
			Content:      []any{map[string]any{"type": "output_text", "text": "calling"}},
			FunctionCall: &dto.FunctionRequest{ID: "legacy_1", Name: "lookup", Arguments: `not-json`},
		},
	}}}

	converted := ResponseOpenAI2Claude(response, strictChatRelayInfo())
	require.Equal(t, "tool_use", converted.StopReason)
	require.Len(t, converted.Content, 2)
	require.Equal(t, "calling", converted.Content[0].GetText())
	require.Equal(t, map[string]any{}, converted.Content[1].Input)
}

func boolPointer(value bool) *bool { return &value }

// TestResponseOpenAIChatCompletions2ClaudeRejectsEmptyChoices avoids empty success responses.
func TestResponseOpenAIChatCompletions2ClaudeRejectsEmptyChoices(t *testing.T) {
	require.Nil(t, ResponseOpenAI2Claude(&dto.OpenAITextResponse{}, strictChatRelayInfo()))
}

// TestStreamResponseOpenAI2ClaudeBuffersToolArgumentsUntilMetadata avoids an invalid tool block when arguments arrive first.
func TestStreamResponseOpenAI2ClaudeBuffersToolArgumentsUntilMetadata(t *testing.T) {
	info := strictChatRelayInfo()
	relaycommon.EnsureClaudeConvertInfo(info)
	info.SendResponseCount = 1
	index := 4

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id: "chatcmpl_1", Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index:    &index,
				Function: dto.FunctionResponse{Arguments: `{"q":"`},
			}}},
		}},
	}, info)
	require.Len(t, first, 1)
	require.Equal(t, "message_start", first[0].Type)

	info.SendResponseCount++
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: &index, ID: "call_1", Type: "function",
				Function: dto.FunctionResponse{Name: "lookup", Arguments: `x"}`},
			}}},
		}},
	}, info)
	require.Len(t, second, 2)
	require.Equal(t, "content_block_start", second[0].Type)
	require.Equal(t, "content_block_delta", second[1].Type)
	require.Equal(t, `{"q":"x"}`, *second[1].Delta.PartialJson)
}

// TestStopOpenBlocksForFinalizeCompletesPendingTools verifies stable IDs and invalid-name dropping at EOF.
func TestStopOpenBlocksForFinalizeCompletesPendingTools(t *testing.T) {
	info := strictChatRelayInfo()
	relaycommon.EnsureClaudeConvertInfo(info)
	info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools
	info.ClaudeConvertInfo.ToolBlocks[2] = &relaycommon.ToolBlockState{AnthropicIndex: 2, OpenAIIndex: 7, Name: "lookup", PendingArgs: `{"q":"x"}`}
	info.ClaudeConvertInfo.ToolBlocks[3] = &relaycommon.ToolBlockState{AnthropicIndex: 3, OpenAIIndex: 9, PendingArgs: `{"q":"discard"}`}

	responses := StopOpenBlocksForFinalize(info)
	require.Len(t, responses, 3)
	require.Equal(t, "content_block_start", responses[0].Type)
	require.Equal(t, "tool_call_7", responses[0].ContentBlock.Id)
	require.Equal(t, "content_block_delta", responses[1].Type)
	require.Equal(t, "content_block_stop", responses[2].Type)
}
