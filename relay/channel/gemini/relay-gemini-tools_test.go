package gemini

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 回归:OpenAI 独有的 id/arguments 不得序列化进 Gemini functionDeclarations,
// 否则上游 400 Unknown name("arguments")。
func TestCovertOpenAI2GeminiFunctionDeclarationsNoOpenAIFields(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	textRequest := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "get_weather",
					Description: "Get weather",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{"city": map[string]any{"type": "string"}},
					},
					// 模拟 tool_use 块回填的真实 arguments
					Arguments: `{"city":"tokyo"}`,
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	geminiRequest, err := CovertOpenAI2Gemini(c, textRequest, info)
	require.NoError(t, err)

	body, err := common.Marshal(geminiRequest)
	require.NoError(t, err)

	var decoded struct {
		Tools []struct {
			FunctionDeclarations []map[string]any `json:"functionDeclarations"`
		} `json:"tools"`
	}
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Len(t, decoded.Tools, 1)
	require.Len(t, decoded.Tools[0].FunctionDeclarations, 1)

	decl := decoded.Tools[0].FunctionDeclarations[0]
	require.Equal(t, "get_weather", decl["name"])
	require.Equal(t, "Get weather", decl["description"])
	require.Contains(t, decl, "parameters")
	require.NotContains(t, decl, "arguments")
	require.NotContains(t, decl, "id")
}

// 空 properties 时 parameters 保持 nil 清理语义(不输出空 schema)。
func TestCovertOpenAI2GeminiEmptyPropertiesDropsParameters(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	textRequest := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name: "noop",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	geminiRequest, err := CovertOpenAI2Gemini(c, textRequest, info)
	require.NoError(t, err)

	body, err := common.Marshal(geminiRequest)
	require.NoError(t, err)

	var decoded struct {
		Tools []struct {
			FunctionDeclarations []map[string]any `json:"functionDeclarations"`
		} `json:"tools"`
	}
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Len(t, decoded.Tools, 1)
	require.Len(t, decoded.Tools[0].FunctionDeclarations, 1)
	require.NotContains(t, decoded.Tools[0].FunctionDeclarations[0], "parameters")
}

// googleSearch/codeExecution/urlContext 特殊工具分流,不进 functionDeclarations。
func TestCovertOpenAI2GeminiSpecialToolsBypass(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	mkTool := func(name string) dto.ToolCallRequest {
		return dto.ToolCallRequest{Type: "function", Function: dto.FunctionRequest{Name: name}}
	}
	textRequest := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
		Tools: []dto.ToolCallRequest{
			mkTool("googleSearch"),
			mkTool("codeExecution"),
			mkTool("urlContext"),
			mkTool("real_tool"),
		},
	}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	geminiRequest, err := CovertOpenAI2Gemini(c, textRequest, info)
	require.NoError(t, err)

	body, err := common.Marshal(geminiRequest)
	require.NoError(t, err)

	var decoded struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Len(t, decoded.Tools, 4)
	require.Contains(t, decoded.Tools[0], "codeExecution")
	require.Contains(t, decoded.Tools[1], "googleSearch")
	require.Contains(t, decoded.Tools[2], "urlContext")
	require.Contains(t, decoded.Tools[3], "functionDeclarations")
}
