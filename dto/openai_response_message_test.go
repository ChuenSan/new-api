package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// TestOpenAITextResponseDecodesRefusalAndLegacyFunctionCall preserves Chat completion response fields used by Claude conversion.
func TestOpenAITextResponseDecodesRefusalAndLegacyFunctionCall(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl-1",
		"model":"gpt-4.1",
		"choices":[{
			"index":0,
			"finish_reason":"stop",
			"message":{
				"role":"assistant",
				"content":[{"type":"refusal","refusal":"I cannot help with that."}],
				"refusal":"I cannot help with that.",
				"function_call":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}
			}
		}]
	}`)

	var response OpenAITextResponse
	require.NoError(t, common.Unmarshal(raw, &response))
	require.Len(t, response.Choices, 1)

	message := response.Choices[0].Message
	require.NotNil(t, message.Refusal)
	require.Equal(t, "I cannot help with that.", *message.Refusal)
	require.Equal(t, "I cannot help with that.", message.GetRefusal())
	require.NotNil(t, message.FunctionCall)
	require.Equal(t, message.FunctionCall, message.ParseFunctionCall())
	require.Equal(t, "weather", message.FunctionCall.Name)
	require.Equal(t, `{"city":"Paris"}`, message.FunctionCall.Arguments)

	parts, ok := message.Content.([]any)
	require.True(t, ok)
	require.Len(t, parts, 1)
	part, ok := parts[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "refusal", part["type"])
	require.Equal(t, "I cannot help with that.", part["refusal"])
}

// TestMessageGetRefusalAggregatesDistinctMessageAndPartRefusals preserves refusal aggregation across Chat response representations.
func TestMessageGetRefusalAggregatesDistinctMessageAndPartRefusals(t *testing.T) {
	messageRefusal := "Message refusal"
	message := Message{
		Refusal: &messageRefusal,
		Content: []any{
			map[string]any{"type": "refusal", "refusal": "Part refusal"},
			map[string]any{"type": "refusal", "refusal": "Message refusal"},
		},
	}

	require.Equal(t, "Message refusal\nPart refusal", message.GetRefusal())
}

// TestMediaContentDecodesRefusalPart preserves typed refusal content-part decoding.
func TestMediaContentDecodesRefusalPart(t *testing.T) {
	var part MediaContent
	require.NoError(t, common.Unmarshal([]byte(`{"type":"refusal","refusal":"No."}`), &part))
	require.Equal(t, "refusal", part.Type)
	require.Equal(t, "No.", part.Refusal)
}

// TestMessageStringContentReadsTypedMediaText preserves text access after SetMediaContent.
func TestMessageStringContentReadsTypedMediaText(t *testing.T) {
	message := Message{}
	message.SetMediaContent([]MediaContent{
		{Type: ContentTypeText, Text: "first"},
		{Type: ContentTypeImageURL},
		{Type: ContentTypeText, Text: "second"},
	})

	require.Equal(t, "firstsecond", message.StringContent())
}

// TestMessageStringContentReadsOutputText preserves Responses-compatible Chat content parts.
func TestMessageStringContentReadsOutputText(t *testing.T) {
	message := Message{Content: []any{
		map[string]any{"type": "output_text", "text": "first"},
		map[string]any{"type": "text", "text": "second"},
	}}

	require.Equal(t, "firstsecond", message.StringContent())
	parts := message.ParseContent()
	require.Len(t, parts, 2)
	require.Equal(t, ContentTypeText, parts[0].Type)
	require.Equal(t, "first", parts[0].Text)
}
