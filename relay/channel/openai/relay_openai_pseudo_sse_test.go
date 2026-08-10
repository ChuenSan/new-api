package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestAggregatePseudoSSEChatCompletion preserves ordered deltas, tools, finish reason, and final usage.
func TestAggregatePseudoSSEChatCompletion(t *testing.T) {
	body := []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"reason "}}]}

data: {"choices":[{"index":0,"delta":{"content":"answer","tool_calls":[{"index":3,"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":3,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}

data: [DONE]
`)

	response, hasUsage, err := aggregatePseudoSSEChatCompletion(body)
	require.NoError(t, err)
	require.True(t, hasUsage)
	require.Equal(t, "chatcmpl-1", response.Id)
	require.Equal(t, "gpt-test", response.Model)
	require.Len(t, response.Choices, 1)
	require.Equal(t, "tool_calls", response.Choices[0].FinishReason)
	require.Equal(t, "answer", response.Choices[0].Message.StringContent())
	require.Equal(t, "reason ", response.Choices[0].Message.GetReasoningContent())
	toolCalls := response.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_1", toolCalls[0].ID)
	require.Equal(t, "weather", toolCalls[0].Function.Name)
	require.Equal(t, `{"city":"Paris"}`, toolCalls[0].Function.Arguments)
	require.Equal(t, 18, response.Usage.TotalTokens)
}

// TestAggregatePseudoSSEChatCompletionRejectsInvalidTerminalStates prevents a partial stream becoming a success response.
func TestAggregatePseudoSSEChatCompletionRejectsInvalidTerminalStates(t *testing.T) {
	_, _, err := aggregatePseudoSSEChatCompletion([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	require.Error(t, err)

	_, _, err = aggregatePseudoSSEChatCompletion([]byte("data: {\"error\":{\"type\":\"upstream_error\",\"message\":\"failed\"}}\n\n"))
	require.Error(t, err)
}

// TestAggregatePseudoSSEChatCompletionAcceptsEitherCompletionSignal preserves valid EOF variants.
func TestAggregatePseudoSSEChatCompletionAcceptsEitherCompletionSignal(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "finish reason",
			body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\n",
		},
		{
			name: "done marker",
			body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete\"}}]}\n\ndata: [DONE]\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, hasUsage, err := aggregatePseudoSSEChatCompletion([]byte(tt.body))
			require.NoError(t, err)
			require.False(t, hasUsage)
			require.Len(t, response.Choices, 1)
			require.Equal(t, "complete", response.Choices[0].Message.StringContent())
		})
	}

	_, _, err := aggregatePseudoSSEChatCompletion([]byte("data: [DONE]\n\n"))
	require.Error(t, err)
}

func newStrictClaudeStreamTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:                              &relaycommon.ChannelMeta{},
		RelayFormat:                              types.RelayFormatClaude,
		RelayMode:                                relayconstant.RelayModeChatCompletions,
		AnthropicMessagesToOpenAIChatCompletions: true,
	}
	relaycommon.EnsureClaudeConvertInfo(info)
	return c, recorder, resp, info
}

// TestOaiStreamHandlerStrictClaudeFlushesAndFinalizesOnce preserves the final chunk and emits one Anthropic terminal sequence.
func TestOaiStreamHandlerStrictClaudeFlushesAndFinalizesOnce(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newStrictClaudeStreamTestContext(t, body)
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 6, usage.TotalTokens)
	require.True(t, info.ClaudeConvertInfo.Done)
	require.True(t, info.ClaudeConvertInfo.HasEmittedMessageDelta)
	output := recorder.Body.String()
	require.Contains(t, output, `"text":"hello"`)
	require.Equal(t, 1, strings.Count(output, "event: message_delta"))
	require.Equal(t, 1, strings.Count(output, "event: message_stop"))
	require.Equal(t, 1, strings.Count(output, "event: content_block_stop"))
}

// TestOaiStreamHandlerStrictClaudeErrorSkipsSuccessTerminal sends one error event without a false successful completion.
func TestOaiStreamHandlerStrictClaudeErrorSkipsSuccessTerminal(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-test","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		``,
		`data: {"error":{"type":"upstream_error","message":"broken upstream"}}`,
		``,
	}, "\n")

	c, recorder, resp, info := newStrictClaudeStreamTestContext(t, body)
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.True(t, info.ClaudeConvertInfo.StreamError)
	output := recorder.Body.String()
	require.Equal(t, 1, strings.Count(output, "event: error"))
	require.NotContains(t, output, "event: message_delta")
	require.NotContains(t, output, "event: message_stop")
}

// TestHandleFinalResponseStrictClaudeWithoutUsage emits zero-valued usage rather than omitting the terminal event.
func TestHandleFinalResponseStrictClaudeWithoutUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, recorder, _, info := newStrictClaudeStreamTestContext(t, "")
	info.ClaudeConvertInfo.HasFinishReason = true
	info.ClaudeConvertInfo.FinishReason = "stop"
	HandleFinalResponse(c, info, "", "", 0, "", "", &dto.Usage{}, false)

	output := recorder.Body.String()
	require.Contains(t, output, `"input_tokens":0`)
	require.Contains(t, output, `"output_tokens":0`)
	require.Equal(t, 1, strings.Count(output, "event: message_delta"))
	require.Equal(t, 1, strings.Count(output, "event: message_stop"))
}

// TestOpenaiHandlerStrictClaudeRejectsEmptyChoices prevents an empty upstream response from becoming a successful null message.
func TestOpenaiHandlerStrictClaudeRejectsEmptyChoices(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, recorder, _, info := newStrictClaudeStreamTestContext(t, "")
	info.IsStream = false
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-1","model":"gpt-test","choices":[]}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, err := OpenaiHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

// TestOpenaiHandlerStrictClaudeKeepsMissingUsageZero separates client usage from billing estimates.
func TestOpenaiHandlerStrictClaudeKeepsMissingUsageZero(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, recorder, _, info := newStrictClaudeStreamTestContext(t, "")
	info.IsStream = false
	info.SetEstimatePromptTokens(13)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl-1","model":"gpt-test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}]}`,
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, err := OpenaiHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 13, usage.PromptTokens)

	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Usage)
	require.Zero(t, response.Usage.InputTokens)
	require.Zero(t, response.Usage.OutputTokens)
}

// TestOpenaiHandlerNonTargetDoesNotUsePseudoSSE keeps fallback isolated to strict Claude.
func TestOpenaiHandlerNonTargetDoesNotUsePseudoSSE(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, recorder, _, info := newStrictClaudeStreamTestContext(t, "")
	info.RelayFormat = types.RelayFormatOpenAI
	info.AnthropicMessagesToOpenAIChatCompletions = false
	info.IsStream = false
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"must not aggregate\"}}]}\n\ndata: [DONE]\n\n",
		)),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, err := OpenaiHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

// TestResponseHasUsageDistinguishesMissingNullAndExplicitZero preserves field presence.
func TestResponseHasUsageDistinguishesMissingNullAndExplicitZero(t *testing.T) {
	require.False(t, responseHasUsage([]byte(`{"choices":[]}`)))
	require.False(t, responseHasUsage([]byte(`{"choices":[],"usage":null}`)))
	require.True(t, responseHasUsage([]byte(`{"choices":[],"usage":{}}`)))
	require.True(t, responseHasUsage([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`)))
}
