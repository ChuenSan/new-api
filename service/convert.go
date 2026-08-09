package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/reasonmap"
	"github.com/samber/lo"
)

func ClaudeToOpenAIRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	if info != nil && info.AnthropicMessagesToOpenAIChatCompletions {
		return claudeToOpenAIChatCompletionsRequest(claudeRequest, info)
	}

	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		Temperature: claudeRequest.Temperature,
	}
	if claudeRequest.MaxTokens != nil {
		openAIRequest.MaxTokens = lo.ToPtr(lo.FromPtr(claudeRequest.MaxTokens))
	}
	if claudeRequest.TopP != nil {
		openAIRequest.TopP = lo.ToPtr(lo.FromPtr(claudeRequest.TopP))
	}
	if claudeRequest.TopK != nil {
		openAIRequest.TopK = lo.ToPtr(lo.FromPtr(claudeRequest.TopK))
	}
	if claudeRequest.Stream != nil {
		openAIRequest.Stream = lo.ToPtr(lo.FromPtr(claudeRequest.Stream))
	}

	isOpenRouter := info.ChannelType == constant.ChannelTypeOpenRouter

	if isOpenRouter {
		if effort := claudeRequest.GetEfforts(); effort != "" {
			effortBytes, _ := json.Marshal(effort)
			openAIRequest.Verbosity = effortBytes
		}
		if claudeRequest.Thinking != nil {
			var reasoning openrouter.RequestReasoning
			if claudeRequest.Thinking.Type == "enabled" {
				reasoning = openrouter.RequestReasoning{
					Enabled:   true,
					MaxTokens: claudeRequest.Thinking.GetBudgetTokens(),
				}
			} else if claudeRequest.Thinking.Type == "adaptive" {
				reasoning = openrouter.RequestReasoning{
					Enabled: true,
				}
			}
			reasoningJSON, err := json.Marshal(reasoning)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
			}
			openAIRequest.Reasoning = reasoningJSON
		}
	} else {
		thinkingSuffix := "-thinking"
		if strings.HasSuffix(info.OriginModelName, thinkingSuffix) &&
			!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
			openAIRequest.Model = openAIRequest.Model + thinkingSuffix
		}
	}

	// Convert stop sequences
	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}

	// Convert tools
	tools, _ := common.Any2Type[[]dto.Tool](claudeRequest.Tools)
	openAITools := make([]dto.ToolCallRequest, 0)
	for _, claudeTool := range tools {
		openAITool := dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        claudeTool.Name,
				Description: claudeTool.Description,
				Parameters:  claudeTool.InputSchema,
			},
		}
		openAITools = append(openAITools, openAITool)
	}
	openAIRequest.Tools = openAITools

	// Convert messages
	openAIMessages := make([]dto.Message, 0)

	// Add system message if present
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() && claudeRequest.GetStringSystem() != "" {
			openAIMessage := dto.Message{
				Role: "system",
			}
			openAIMessage.SetStringContent(claudeRequest.GetStringSystem())
			openAIMessages = append(openAIMessages, openAIMessage)
		} else {
			systems := claudeRequest.ParseSystem()
			if len(systems) > 0 {
				openAIMessage := dto.Message{
					Role: "system",
				}
				isOpenRouterClaude := isOpenRouter && strings.HasPrefix(info.UpstreamModelName, "anthropic/claude")
				if isOpenRouterClaude {
					systemMediaMessages := make([]dto.MediaContent, 0, len(systems))
					for _, system := range systems {
						message := dto.MediaContent{
							Type:         "text",
							Text:         system.GetText(),
							CacheControl: system.CacheControl,
						}
						systemMediaMessages = append(systemMediaMessages, message)
					}
					openAIMessage.SetMediaContent(systemMediaMessages)
				} else {
					systemStr := ""
					for _, system := range systems {
						if system.Text != nil {
							systemStr += *system.Text
						}
					}
					openAIMessage.SetStringContent(systemStr)
				}
				openAIMessages = append(openAIMessages, openAIMessage)
			}
		}
	}
	for _, claudeMessage := range claudeRequest.Messages {
		openAIMessage := dto.Message{
			Role: claudeMessage.Role,
		}

		//log.Printf("claudeMessage.Content: %v", claudeMessage.Content)
		if claudeMessage.IsStringContent() {
			openAIMessage.SetStringContent(claudeMessage.GetStringContent())
		} else {
			content, err := claudeMessage.ParseContent()
			if err != nil {
				return nil, err
			}
			contents := content
			var toolCalls []dto.ToolCallRequest
			mediaMessages := make([]dto.MediaContent, 0, len(contents))

			for _, mediaMsg := range contents {
				switch mediaMsg.Type {
				case "text", "input_text":
					message := dto.MediaContent{
						Type:         "text",
						Text:         mediaMsg.GetText(),
						CacheControl: mediaMsg.CacheControl,
					}
					mediaMessages = append(mediaMessages, message)
				case "image":
					// Handle image conversion (base64 to URL or keep as is)
					imageData := fmt.Sprintf("data:%s;base64,%s", mediaMsg.Source.MediaType, mediaMsg.Source.Data)
					//textContent += fmt.Sprintf("[Image: %s]", imageData)
					mediaMessage := dto.MediaContent{
						Type:     "image_url",
						ImageUrl: &dto.MessageImageUrl{Url: imageData},
					}
					mediaMessages = append(mediaMessages, mediaMessage)
				case "tool_use":
					toolCall := dto.ToolCallRequest{
						ID:   mediaMsg.Id,
						Type: "function",
						Function: dto.FunctionRequest{
							Name:      mediaMsg.Name,
							Arguments: toJSONString(mediaMsg.Input),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				case "tool_result":
					// Add tool result as a separate message
					toolName := mediaMsg.Name
					if toolName == "" {
						toolName = claudeRequest.SearchToolNameByToolCallId(mediaMsg.ToolUseId)
					}
					oaiToolMessage := dto.Message{
						Role:       "tool",
						Name:       &toolName,
						ToolCallId: mediaMsg.ToolUseId,
					}
					//oaiToolMessage.SetStringContent(*mediaMsg.GetMediaContent().Text)
					if mediaMsg.IsStringContent() {
						oaiToolMessage.SetStringContent(mediaMsg.GetStringContent())
					} else {
						mediaContents := mediaMsg.ParseMediaContent()
						encodeJson, _ := common.Marshal(mediaContents)
						oaiToolMessage.SetStringContent(string(encodeJson))
					}
					openAIMessages = append(openAIMessages, oaiToolMessage)
				}
			}

			if len(toolCalls) > 0 {
				openAIMessage.SetToolCalls(toolCalls)
			}

			if len(mediaMessages) > 0 && len(toolCalls) == 0 {
				openAIMessage.SetMediaContent(mediaMessages)
			}
		}
		if len(openAIMessage.ParseContent()) > 0 || len(openAIMessage.ToolCalls) > 0 {
			openAIMessages = append(openAIMessages, openAIMessage)
		}
	}

	openAIRequest.Messages = openAIMessages

	return &openAIRequest, nil
}

func claudeToOpenAIChatCompletionsRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		Temperature: claudeRequest.Temperature,
	}
	if claudeRequest.MaxTokens != nil {
		openAIRequest.MaxTokens = lo.ToPtr(lo.FromPtr(claudeRequest.MaxTokens))
	}
	if claudeRequest.TopP != nil {
		openAIRequest.TopP = lo.ToPtr(lo.FromPtr(claudeRequest.TopP))
	}
	if claudeRequest.TopK != nil {
		openAIRequest.TopK = lo.ToPtr(lo.FromPtr(claudeRequest.TopK))
	}
	if claudeRequest.Stream != nil {
		openAIRequest.Stream = lo.ToPtr(lo.FromPtr(claudeRequest.Stream))
	}
	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}

	tools, err := strictClaudeTools(claudeRequest.Tools)
	if err != nil {
		return nil, err
	}
	openAIRequest.Tools = tools
	if len(tools) > 0 {
		choice, parallel, err := strictClaudeToolChoice(claudeRequest.ToolChoice)
		if err != nil {
			return nil, err
		}
		openAIRequest.ToolChoice = choice
		openAIRequest.ParallelTooCalls = parallel
	}
	applyStrictClaudeReasoning(&openAIRequest, claudeRequest, info)

	messages := make([]dto.Message, 0, len(claudeRequest.Messages)+1)
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() {
			if system := claudeRequest.GetStringSystem(); system != "" {
				message := dto.Message{Role: "system"}
				message.SetStringContent(system)
				messages = append(messages, message)
			}
		} else {
			systems := claudeRequest.ParseSystem()
			isOpenRouterClaude := info != nil && info.ChannelMeta != nil &&
				info.ChannelType == constant.ChannelTypeOpenRouter && strings.HasPrefix(info.UpstreamModelName, "anthropic/claude")
			if isOpenRouterClaude {
				content := make([]dto.MediaContent, 0, len(systems))
				for _, system := range systems {
					if text := system.GetText(); text != "" {
						content = append(content, dto.MediaContent{Type: "text", Text: text, CacheControl: system.CacheControl})
					}
				}
				if len(content) > 0 {
					message := dto.Message{Role: "system"}
					message.SetMediaContent(content)
					messages = append(messages, message)
				}
			} else {
				parts := make([]string, 0, len(systems))
				for _, system := range systems {
					if text := system.GetText(); text != "" {
						parts = append(parts, text)
					}
				}
				if len(parts) > 0 {
					message := dto.Message{Role: "system"}
					message.SetStringContent(strings.Join(parts, "\n"))
					messages = append(messages, message)
				}
			}
		}
	}

	for _, claudeMessage := range claudeRequest.Messages {
		message, toolResults, err := strictClaudeMessage(claudeMessage, info)
		if err != nil {
			return nil, err
		}
		if message != nil {
			messages = append(messages, *message)
		}
		messages = append(messages, toolResults...)
	}
	openAIRequest.Messages = messages
	return &openAIRequest, nil
}

func strictClaudeTools(raw any) ([]dto.ToolCallRequest, error) {
	if raw == nil {
		return nil, nil
	}
	tools, err := common.Any2Type[[]dto.Tool](raw)
	if err != nil {
		return nil, fmt.Errorf("invalid Claude tools: %w", err)
	}
	result := make([]dto.ToolCallRequest, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("Claude tool name is required")
		}
		if tool.InputSchema == nil {
			return nil, fmt.Errorf("Claude tool %q input_schema must be an object", tool.Name)
		}
		schema := make(map[string]any, len(tool.InputSchema))
		for key, value := range tool.InputSchema {
			schema[key] = removeURIFormat(value)
		}
		result = append(result, dto.ToolCallRequest{Type: "function", Function: dto.FunctionRequest{Name: tool.Name, Description: tool.Description, Parameters: schema}})
	}
	return result, nil
}

func removeURIFormat(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, item := range current {
			if key == "format" && item == "uri" {
				continue
			}
			result[key] = removeURIFormat(item)
		}
		return result
	case []any:
		result := make([]any, len(current))
		for index, item := range current {
			result[index] = removeURIFormat(item)
		}
		return result
	default:
		return value
	}
}

func strictClaudeToolChoice(raw any) (any, *bool, error) {
	if raw == nil {
		return nil, nil, nil
	}
	choice, err := common.Any2Type[dto.ClaudeToolChoice](raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Claude tool_choice: %w", err)
	}
	var openAIChoice any
	switch choice.Type {
	case "auto":
		openAIChoice = "auto"
	case "any":
		openAIChoice = "required"
	case "none":
		openAIChoice = "none"
	case "tool":
		if strings.TrimSpace(choice.Name) == "" {
			return nil, nil, fmt.Errorf("Claude tool_choice tool name is required")
		}
		openAIChoice = map[string]any{"type": "function", "function": map[string]string{"name": choice.Name}}
	default:
		return nil, nil, fmt.Errorf("unsupported Claude tool_choice type %q", choice.Type)
	}
	if choice.DisableParallelToolUse == nil {
		return openAIChoice, nil, nil
	}
	parallel := !*choice.DisableParallelToolUse
	return openAIChoice, &parallel, nil
}

func applyStrictClaudeReasoning(openAIRequest *dto.GeneralOpenAIRequest, claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) {
	if openAIRequest == nil || info == nil {
		return
	}
	if info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter {
		reasoning := openrouter.RequestReasoning{}
		hasReasoning := false
		if claudeRequest.Thinking != nil {
			switch claudeRequest.Thinking.Type {
			case "enabled":
				reasoning.Enabled = true
				reasoning.MaxTokens = claudeRequest.Thinking.GetBudgetTokens()
				hasReasoning = true
			case "adaptive":
				reasoning.Enabled = true
				hasReasoning = true
			}
		}
		if effort := claudeRequest.GetEfforts(); effort != "" {
			reasoning.Effort = effort
			reasoning.Enabled = true
			hasReasoning = true
		}
		if hasReasoning {
			openAIRequest.Reasoning, _ = json.Marshal(reasoning)
		}
		return
	}
	if effort := claudeRequest.GetEfforts(); effort != "" {
		openAIRequest.ReasoningEffort = effort
	}
}

func strictClaudeMessage(claudeMessage dto.ClaudeMessage, info *relaycommon.RelayInfo) (*dto.Message, []dto.Message, error) {
	message := dto.Message{Role: claudeMessage.Role}
	if claudeMessage.IsStringContent() {
		message.SetStringContent(claudeMessage.GetStringContent())
		return &message, nil, nil
	}
	content, err := claudeMessage.ParseContent()
	if err != nil {
		return nil, nil, err
	}
	media := make([]dto.MediaContent, 0, len(content))
	toolCalls := make([]dto.ToolCallRequest, 0)
	toolResults := make([]dto.Message, 0)
	reasoningDetails := make([]map[string]any, 0)
	for _, block := range content {
		switch block.Type {
		case "text", "input_text":
			media = append(media, dto.MediaContent{Type: "text", Text: block.GetText(), CacheControl: block.CacheControl})
		case "thinking":
			if info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter {
				text := block.GetText()
				if block.Thinking != nil {
					text = *block.Thinking
				}
				if text != "" {
					detail := map[string]any{"type": "reasoning.text", "text": text, "format": "anthropic-claude-v1"}
					if block.Signature != "" {
						detail["signature"] = block.Signature
					}
					reasoningDetails = append(reasoningDetails, detail)
				}
			}
		case "redacted_thinking":
			if info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter {
				if data := common.Interface2String(block.Data); data != "" {
					reasoningDetails = append(reasoningDetails, map[string]any{"type": "reasoning.encrypted", "data": data, "format": "anthropic-claude-v1"})
				}
			}
		case "image":
			image, err := strictClaudeImage(block.Source)
			if err != nil {
				return nil, nil, err
			}
			media = append(media, dto.MediaContent{Type: "image_url", ImageUrl: &dto.MessageImageUrl{Url: image}})
		case "tool_use":
			if strings.TrimSpace(block.Name) == "" {
				return nil, nil, fmt.Errorf("Claude tool_use name is required")
			}
			arguments, err := json.Marshal(block.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("marshal Claude tool_use input: %w", err)
			}
			toolCalls = append(toolCalls, dto.ToolCallRequest{ID: block.Id, Type: "function", Function: dto.FunctionRequest{Name: block.Name, Arguments: string(arguments)}})
		case "tool_result":
			result := dto.Message{Role: "tool", ToolCallId: block.ToolUseId}
			if block.IsStringContent() {
				result.SetStringContent(block.GetStringContent())
			} else {
				encoded, err := json.Marshal(block.Content)
				if err != nil {
					return nil, nil, fmt.Errorf("marshal Claude tool_result content: %w", err)
				}
				result.SetStringContent(string(encoded))
			}
			toolResults = append(toolResults, result)
		}
	}
	if len(reasoningDetails) > 0 {
		encoded, err := json.Marshal(reasoningDetails)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal OpenRouter reasoning details: %w", err)
		}
		message.ReasoningDetails = encoded
	}
	if len(media) > 0 {
		message.SetMediaContent(media)
	}
	if len(toolCalls) > 0 {
		message.SetToolCalls(toolCalls)
		if len(media) == 0 {
			message.SetNullContent()
		}
	}
	if message.Content == nil && len(toolCalls) == 0 && len(message.ReasoningDetails) == 0 {
		return nil, toolResults, nil
	}
	return &message, toolResults, nil
}

func strictClaudeImage(source *dto.ClaudeMessageSource) (string, error) {
	if source == nil {
		return "", fmt.Errorf("Claude image source is required")
	}
	switch source.Type {
	case "base64":
		data := common.Interface2String(source.Data)
		if source.MediaType == "" || data == "" {
			return "", fmt.Errorf("Claude base64 image media_type and data are required")
		}
		return fmt.Sprintf("data:%s;base64,%s", source.MediaType, data), nil
	case "url":
		if source.Url == "" {
			return "", fmt.Errorf("Claude image URL is required")
		}
		return source.Url, nil
	default:
		return "", fmt.Errorf("unsupported Claude image source type %q", source.Type)
	}
}

func generateStopBlock(index int) *dto.ClaudeResponse {
	return &dto.ClaudeResponse{
		Type:  "content_block_stop",
		Index: common.GetPointer[int](index),
	}
}

func buildClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	return BuildClaudeUsageFromOpenAIUsage(oaiUsage)
}

func BuildClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	if oaiUsage == nil {
		return &dto.ClaudeUsage{}
	}
	cachedRead := oaiUsage.PromptTokensDetails.CachedTokens
	if cachedRead == 0 {
		cachedRead = oaiUsage.PromptCacheHitTokens
	}
	creationRaw := oaiUsage.ClaudeCacheCreation5mTokens + oaiUsage.ClaudeCacheCreation1hTokens
	cacheCreation := max(oaiUsage.PromptTokensDetails.CachedCreationTokens, creationRaw)
	cacheCreation5m, cacheCreation1h := NormalizeCacheCreationSplit(cacheCreation, oaiUsage.ClaudeCacheCreation5mTokens, oaiUsage.ClaudeCacheCreation1hTokens)
	usage := &dto.ClaudeUsage{
		InputTokens:              max(0, oaiUsage.PromptTokens-cachedRead-cacheCreation),
		OutputTokens:             max(0, oaiUsage.CompletionTokens),
		CacheCreationInputTokens: max(0, cacheCreation),
		CacheReadInputTokens:     max(0, cachedRead),
	}
	if cacheCreation5m > 0 || cacheCreation1h > 0 {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: max(0, cacheCreation5m),
			Ephemeral1hInputTokens: max(0, cacheCreation1h),
		}
	}
	return usage
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	remainder := lo.Max([]int{totalTokens - tokens5m - tokens1h, 0})
	return tokens5m + remainder, tokens1h
}

func StopReasonOpenAI2Claude(reason string) string { return stopReasonOpenAI2Claude(reason) }

func StrictStopReasonOpenAI2Claude(reason string) string { return strictClaudeStopReason(reason) }

func StopOpenBlocksForFinalize(info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	if info == nil || info.ClaudeConvertInfo == nil {
		return nil
	}
	state := info.ClaudeConvertInfo
	var responses []*dto.ClaudeResponse
	switch state.LastMessagesType {
	case relaycommon.LastMessageTypeText, relaycommon.LastMessageTypeThinking:
		if state.Index > 0 {
			responses = append(responses, generateStopBlock(state.Index-1))
		}
	case relaycommon.LastMessageTypeTools:
		started := make([]*relaycommon.ToolBlockState, 0, len(state.ToolBlocks))
		pending := make([]*relaycommon.ToolBlockState, 0, len(state.ToolBlocks))
		for _, tool := range state.ToolBlocks {
			if tool == nil {
				continue
			}
			if tool.Started {
				started = append(started, tool)
			} else {
				pending = append(pending, tool)
			}
		}
		sort.Slice(started, func(i, j int) bool {
			return started[i].AnthropicIndex < started[j].AnthropicIndex
		})
		for _, tool := range started {
			responses = append(responses, generateStopBlock(tool.AnthropicIndex))
		}
		sort.Slice(pending, func(i, j int) bool {
			return pending[i].Order < pending[j].Order
		})
		for _, tool := range pending {
			if tool.Name == "" {
				common.SysError(fmt.Sprintf("dropping pending tool call at upstream index %d without a name", tool.OpenAIIndex))
				continue
			}
			if tool.ID == "" {
				tool.ID = fmt.Sprintf("tool_call_%d", tool.OpenAIIndex)
			}
			if tool.AnthropicIndex < 0 {
				tool.AnthropicIndex = state.Index
				state.Index++
				state.ToolBlockIndexByOpenAIIndex[tool.OpenAIIndex] = tool.AnthropicIndex
			}
			tool.Started = true
			state.ToolBlockStarted[tool.AnthropicIndex] = true
			blockIndex := tool.AnthropicIndex
			responses = append(responses, &dto.ClaudeResponse{Index: &blockIndex, Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Id: tool.ID, Type: "tool_use", Name: tool.Name, Input: map[string]interface{}{}}})
			if tool.PendingArgs != "" {
				args := tool.PendingArgs
				responses = append(responses, &dto.ClaudeResponse{Index: &blockIndex, Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &args}})
			}
			responses = append(responses, generateStopBlock(blockIndex))
		}
	}
	state.ToolBlockStarted = make(map[int]bool)
	state.ToolBlockIndexByOpenAIIndex = make(map[int]int)
	state.ToolBlocks = make(map[int]*relaycommon.ToolBlockState)
	state.LastMessagesType = relaycommon.LastMessageTypeNone
	return responses
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	if openAIResponse == nil || info == nil || info.ClaudeConvertInfo == nil {
		return nil
	}
	if !info.AnthropicMessagesToOpenAIChatCompletions {
		return streamResponseOpenAI2ClaudeLegacy(openAIResponse, info)
	}
	state := info.ClaudeConvertInfo
	if state.Done || state.StreamError || state.HasEmittedMessageDelta {
		return nil
	}
	if openAIResponse.Usage != nil {
		state.Usage = openAIResponse.Usage
		state.HasUpstreamUsage = true
	}

	var responses []*dto.ClaudeResponse
	stopOpenBlocks := func() {
		responses = append(responses, StopOpenBlocksForFinalize(info)...)
	}
	startMessage := func() {
		if info.SendResponseCount != 1 {
			return
		}
		message := &dto.ClaudeMediaMessage{Id: openAIResponse.Id, Model: openAIResponse.Model, Type: "message", Role: "assistant", Usage: &dto.ClaudeUsage{InputTokens: info.GetEstimatePromptTokens()}}
		message.SetContent(make([]any, 0))
		responses = append(responses, &dto.ClaudeResponse{Type: "message_start", Message: message})
	}
	assignTool := func(openAIIndex int) *relaycommon.ToolBlockState {
		if tool, ok := state.ToolBlocks[openAIIndex]; ok {
			return tool
		}
		tool := &relaycommon.ToolBlockState{
			AnthropicIndex: -1,
			OpenAIIndex:    openAIIndex,
			Order:          len(state.ToolBlocks),
		}
		state.ToolBlocks[openAIIndex] = tool
		return tool
	}
	emitToolStart := func(tool *relaycommon.ToolBlockState) {
		if tool == nil || tool.Started || tool.ID == "" || tool.Name == "" {
			return
		}
		if tool.AnthropicIndex < 0 {
			tool.AnthropicIndex = state.Index
			state.Index++
			state.ToolBlockIndexByOpenAIIndex[tool.OpenAIIndex] = tool.AnthropicIndex
		}
		tool.Started = true
		state.ToolBlockStarted[tool.AnthropicIndex] = true
		index := tool.AnthropicIndex
		responses = append(responses, &dto.ClaudeResponse{Index: &index, Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Id: tool.ID, Type: "tool_use", Name: tool.Name, Input: map[string]interface{}{}}})
		if tool.PendingArgs != "" {
			args := tool.PendingArgs
			responses = append(responses, &dto.ClaudeResponse{Index: &index, Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &args}})
			tool.PendingArgs = ""
		}
	}
	emitTools := func(toolCalls []dto.ToolCallResponse) {
		if len(toolCalls) == 0 {
			return
		}
		if state.LastMessagesType != relaycommon.LastMessageTypeTools {
			stopOpenBlocks()
			state.LastMessagesType = relaycommon.LastMessageTypeTools
		}
		for position := range toolCalls {
			toolCall := &toolCalls[position]
			openAIIndex := position
			if toolCall.Index != nil {
				openAIIndex = *toolCall.Index
			}
			tool := assignTool(openAIIndex)
			if toolCall.ID != "" {
				tool.ID = toolCall.ID
			}
			if toolCall.Function.Name != "" {
				tool.Name = toolCall.Function.Name
			}
			tool.PendingArgs += toolCall.Function.Arguments
			emitToolStart(tool)
		}
	}
	emitContent := func(reasoning, content string) {
		blockType, deltaType, value := relaycommon.LastMessageTypeText, "text_delta", content
		if reasoning != "" {
			blockType, deltaType, value = relaycommon.LastMessageTypeThinking, "thinking_delta", reasoning
		}
		if value == "" {
			return
		}
		if state.LastMessagesType != blockType {
			stopOpenBlocks()
			index := state.Index
			state.Index++
			block := &dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer("")}
			if blockType == relaycommon.LastMessageTypeThinking {
				block = &dto.ClaudeMediaMessage{Type: "thinking", Thinking: common.GetPointer("")}
			}
			responses = append(responses, &dto.ClaudeResponse{Index: &index, Type: "content_block_start", ContentBlock: block})
			state.LastMessagesType = blockType
		}
		index := state.Index - 1
		delta := &dto.ClaudeMediaMessage{Type: deltaType}
		if blockType == relaycommon.LastMessageTypeThinking {
			delta.Thinking = &value
		} else {
			delta.Text = &value
		}
		responses = append(responses, &dto.ClaudeResponse{Index: &index, Type: "content_block_delta", Delta: delta})
	}

	startMessage()
	if len(openAIResponse.Choices) == 0 {
		return responses
	}
	choice := openAIResponse.Choices[0]
	emitTools(choice.Delta.ToolCalls)
	emitContent(choice.Delta.GetReasoningContent(), choice.Delta.GetContentString())
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		if !state.HasFinishReason {
			state.FinishReason = *choice.FinishReason
			state.HasFinishReason = true
			info.FinishReason = *choice.FinishReason
		}
		for _, tool := range state.ToolBlocks {
			emitToolStart(tool)
		}
	}
	return responses
}

func streamResponseOpenAI2ClaudeLegacy(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {

	if info.ClaudeConvertInfo.Done {
		return nil
	}

	var claudeResponses []*dto.ClaudeResponse
	// stopOpenBlocks emits the required content_block_stop event(s) for the currently open block(s)
	// according to Anthropic's SSE streaming state machine:
	// content_block_start -> content_block_delta* -> content_block_stop (per index).
	//
	// For text/thinking, there is at most one open block at info.ClaudeConvertInfo.Index.
	// For tools, only blocks recorded in ToolBlockStarted are closed — the upstream
	// tool_calls[].index may not start at 0 or be contiguous, so we must never emit a
	// stop for an index that never received a start.
	stopOpenBlocks := func() {
		switch info.ClaudeConvertInfo.LastMessagesType {
		case relaycommon.LastMessageTypeText, relaycommon.LastMessageTypeThinking:
			// Index is advanced past the current block at start time, so the open
			// text/thinking block lives at Index-1.
			claudeResponses = append(claudeResponses, generateStopBlock(info.ClaudeConvertInfo.Index-1))
		case relaycommon.LastMessageTypeTools:
			started := make([]int, 0, len(info.ClaudeConvertInfo.ToolBlockStarted))
			for idx := range info.ClaudeConvertInfo.ToolBlockStarted {
				started = append(started, idx)
			}
			sort.Ints(started)
			for _, idx := range started {
				claudeResponses = append(claudeResponses, generateStopBlock(idx))
			}
			info.ClaudeConvertInfo.ToolBlockStarted = make(map[int]bool)
			info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex = make(map[int]int)
		}
	}
	// stopOpenBlocksAndAdvance closes the currently open block(s) and advances the content block index
	// to the next available slot for subsequent content_block_start events.
	//
	// This prevents invalid streams where a content_block_delta (e.g. thinking_delta) is emitted for an
	// index whose active content_block type is different (the typical cause of "Mismatched content block type").
	// Index is advanced at allocation time for every block type: assignToolBlockIndex
	// increments it for tool blocks, and each text/thinking content_block_start site
	// increments it after capturing the slot. stopOpenBlocksAndAdvance therefore never
	// recomputes Index manually.
	stopOpenBlocksAndAdvance := func() {
		if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeNone {
			return
		}
		stopOpenBlocks()
		// Index is already advanced at allocation time for every block type (text/thinking
		// increment it when their block starts; tool blocks increment it inside
		// assignToolBlockIndex), so no manual recompute is needed here.
		info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeNone
	}
	// assignToolBlockIndex returns the Claude content_block index for the given upstream
	// OpenAI tool_calls[].index, allocating a fresh dense index (and emitting a
	// content_block_start) on first sight of that upstream index. Subsequent chunks for
	// the same upstream index reuse the already-allocated block.
	assignToolBlockIndex := func(openAIIndex int, toolCall *dto.ToolCallResponse) int {
		if claudeIdx, ok := info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex[openAIIndex]; ok {
			return claudeIdx
		}
		claudeIdx := info.ClaudeConvertInfo.Index
		info.ClaudeConvertInfo.Index++
		info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex[openAIIndex] = claudeIdx
		info.ClaudeConvertInfo.ToolBlockStarted[claudeIdx] = true
		return claudeIdx
	}
	if info.SendResponseCount == 1 {
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: &dto.ClaudeUsage{
				InputTokens:  info.GetEstimatePromptTokens(),
				OutputTokens: 0,
			},
		}
		msg.SetContent(make([]any, 0))
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type:    "message_start",
			Message: msg,
		})
		//claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
		//	Type: "ping",
		//})
		if openAIResponse.IsToolCall() {
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools
			var toolCall dto.ToolCallResponse
			if len(openAIResponse.Choices) > 0 && len(openAIResponse.Choices[0].Delta.ToolCalls) > 0 {
				toolCall = openAIResponse.Choices[0].Delta.ToolCalls[0]
			} else {
				first := openAIResponse.GetFirstToolCall()
				if first != nil {
					toolCall = *first
				} else {
					toolCall = dto.ToolCallResponse{}
				}
			}
			openAIIndex := 0
			if toolCall.Index != nil {
				openAIIndex = *toolCall.Index
			}
			allocatedIdx := assignToolBlockIndex(openAIIndex, &toolCall)
			resp := &dto.ClaudeResponse{
				Type: "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Id:    toolCall.ID,
					Type:  "tool_use",
					Name:  toolCall.Function.Name,
					Input: map[string]interface{}{},
				},
			}
			resp.SetIndex(allocatedIdx)
			claudeResponses = append(claudeResponses, resp)
			// 首块包含工具 delta，则追加 input_json_delta
			if toolCall.Function.Arguments != "" {
				idx := allocatedIdx
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:        "input_json_delta",
						PartialJson: &toolCall.Function.Arguments,
					},
				})
			}
		} else {

		}
		// 判断首个响应是否存在内容（非标准的 OpenAI 响应）
		if len(openAIResponse.Choices) > 0 {
			reasoning := openAIResponse.Choices[0].Delta.GetReasoningContent()
			content := openAIResponse.Choices[0].Delta.GetContentString()

			if reasoning != "" {
				if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeThinking {
					stopOpenBlocksAndAdvance()
				}
				idx := info.ClaudeConvertInfo.Index
				info.ClaudeConvertInfo.Index++
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_start",
					ContentBlock: &dto.ClaudeMediaMessage{
						Type:     "thinking",
						Thinking: common.GetPointer[string](""),
					},
				})
				idx2 := idx
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx2,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type:     "thinking_delta",
						Thinking: &reasoning,
					},
				})
				info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeThinking
			} else if content != "" {
				if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeText {
					stopOpenBlocksAndAdvance()
				}
				idx := info.ClaudeConvertInfo.Index
				info.ClaudeConvertInfo.Index++
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_start",
					ContentBlock: &dto.ClaudeMediaMessage{
						Type: "text",
						Text: common.GetPointer[string](""),
					},
				})
				idx2 := idx
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx2,
					Type:  "content_block_delta",
					Delta: &dto.ClaudeMediaMessage{
						Type: "text_delta",
						Text: common.GetPointer[string](content),
					},
				})
				info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText
			}
		}

		// 如果首块就带 finish_reason，需要立即发送停止块
		if len(openAIResponse.Choices) > 0 && openAIResponse.Choices[0].FinishReason != nil && *openAIResponse.Choices[0].FinishReason != "" {
			info.FinishReason = *openAIResponse.Choices[0].FinishReason
			stopOpenBlocks()
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = info.ClaudeConvertInfo.Usage
			}
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
		}
		return claudeResponses
	}

	if len(openAIResponse.Choices) == 0 {
		// Some OpenAI-compatible upstreams end with a usage-only SSE chunk.
		oaiUsage := openAIResponse.Usage
		if oaiUsage == nil {
			oaiUsage = info.ClaudeConvertInfo.Usage
		}
		if oaiUsage != nil {
			stopOpenBlocks()
			stopReason := stopReasonOpenAI2Claude(info.FinishReason)
			if stopReason == "" {
				stopReason = "end_turn"
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
				Delta: &dto.ClaudeMediaMessage{
					StopReason: common.GetPointer[string](stopReason),
				},
			})
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
		}
		return claudeResponses
	} else {
		chosenChoice := openAIResponse.Choices[0]
		doneChunk := chosenChoice.FinishReason != nil && *chosenChoice.FinishReason != ""
		if doneChunk {
			info.FinishReason = *chosenChoice.FinishReason
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = info.ClaudeConvertInfo.Usage
				// Some upstreams emit finish_reason first, then send a final usage-only chunk.
				// Defer closing until usage is available so the final message_delta carries it.
				return claudeResponses
			}
		}

		var claudeResponse dto.ClaudeResponse
		var isEmpty bool
		claudeResponse.Type = "content_block_delta"
		if len(chosenChoice.Delta.ToolCalls) > 0 {
			toolCalls := chosenChoice.Delta.ToolCalls
			if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeTools {
				stopOpenBlocksAndAdvance()
			}
			info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools

			for i := range toolCalls {
				toolCall := toolCalls[i]
				openAIIndex := i
				if toolCall.Index != nil {
					openAIIndex = *toolCall.Index
				}
				isNewBlock := false
				if _, ok := info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex[openAIIndex]; !ok {
					isNewBlock = true
				}
				idx := assignToolBlockIndex(openAIIndex, &toolCall)

				if isNewBlock {
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: &idx,
						Type:  "content_block_start",
						ContentBlock: &dto.ClaudeMediaMessage{
							Id:    toolCall.ID,
							Type:  "tool_use",
							Name:  toolCall.Function.Name,
							Input: map[string]interface{}{},
						},
					})
				}

				if len(toolCall.Function.Arguments) > 0 {
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: &idx,
						Type:  "content_block_delta",
						Delta: &dto.ClaudeMediaMessage{
							Type:        "input_json_delta",
							PartialJson: &toolCall.Function.Arguments,
						},
					})
				}
			}
		} else {
			reasoning := chosenChoice.Delta.GetReasoningContent()
			textContent := chosenChoice.Delta.GetContentString()
			if reasoning != "" || textContent != "" {
				if reasoning != "" {
					if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeThinking {
						stopOpenBlocksAndAdvance()
						idx := info.ClaudeConvertInfo.Index
						info.ClaudeConvertInfo.Index++
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type:     "thinking",
								Thinking: common.GetPointer[string](""),
							},
						})
					}
					info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeThinking
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type:     "thinking_delta",
						Thinking: &reasoning,
					}
				} else {
					if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeText {
						stopOpenBlocksAndAdvance()
						idx := info.ClaudeConvertInfo.Index
						info.ClaudeConvertInfo.Index++
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type: "text",
								Text: common.GetPointer[string](""),
							},
						})
					}
					info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type: "text_delta",
						Text: common.GetPointer[string](textContent),
					}
				}
			} else {
				isEmpty = true
			}
		}

		// Capture the delta's target index after any block-type transition above, so a
		// tool-call chunk that follows a text/thinking block does not stamp its
		// (empty) delta with the advanced Index of a not-yet-opened block.
		if claudeResponse.Delta != nil {
			claudeResponse.Index = common.GetPointer[int](info.ClaudeConvertInfo.Index - 1)
		}
		if !isEmpty && claudeResponse.Delta != nil {
			claudeResponses = append(claudeResponses, &claudeResponse)
		}

		if doneChunk || info.ClaudeConvertInfo.Done {
			stopOpenBlocks()
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = info.ClaudeConvertInfo.Usage
			}
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
			return claudeResponses
		}
	}

	return claudeResponses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	if info != nil && info.AnthropicMessagesToOpenAIChatCompletions {
		return responseOpenAIChatCompletions2Claude(openAIResponse)
	}
	var stopReason string
	contents := make([]dto.ClaudeMediaMessage, 0)
	claudeResponse := &dto.ClaudeResponse{
		Id:    openAIResponse.Id,
		Type:  "message",
		Role:  "assistant",
		Model: openAIResponse.Model,
	}
	for _, choice := range openAIResponse.Choices {
		stopReason = stopReasonOpenAI2Claude(choice.FinishReason)
		textContent := choice.Message.StringContent()
		toolCalls := choice.Message.ParseToolCalls()
		if textContent != "" || len(toolCalls) == 0 {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "text"
			claudeContent.SetText(textContent)
			contents = append(contents, claudeContent)
		}
		for _, toolUse := range toolCalls {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "tool_use"
			claudeContent.Id = toolUse.ID
			claudeContent.Name = toolUse.Function.Name
			mapParams := map[string]interface{}{}
			if strings.TrimSpace(toolUse.Function.Arguments) != "" {
				var parsed map[string]interface{}
				if err := common.Unmarshal([]byte(toolUse.Function.Arguments), &parsed); err == nil && parsed != nil {
					mapParams = parsed
				}
			}
			claudeContent.Input = mapParams
			contents = append(contents, claudeContent)
		}
	}
	claudeResponse.Content = contents
	claudeResponse.StopReason = stopReason
	claudeResponse.Usage = buildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage)

	return claudeResponse
}

func responseOpenAIChatCompletions2Claude(openAIResponse *dto.OpenAITextResponse) *dto.ClaudeResponse {
	if openAIResponse == nil || len(openAIResponse.Choices) == 0 {
		return nil
	}
	choice := openAIResponse.Choices[0]
	response := &dto.ClaudeResponse{
		Id:         openAIResponse.Id,
		Type:       "message",
		Role:       "assistant",
		Model:      openAIResponse.Model,
		StopReason: strictClaudeStopReason(choice.FinishReason),
		Usage:      BuildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage),
	}
	content := make([]dto.ClaudeMediaMessage, 0)
	if reasoning := choice.Message.GetReasoningContent(); reasoning != "" {
		content = append(content, dto.ClaudeMediaMessage{Type: "thinking", Thinking: &reasoning})
	}
	if text := choice.Message.StringContent(); text != "" {
		textBlock := dto.ClaudeMediaMessage{Type: "text"}
		textBlock.SetText(text)
		content = append(content, textBlock)
	}
	if refusal := choice.Message.GetRefusal(); refusal != "" {
		refusalBlock := dto.ClaudeMediaMessage{Type: "text"}
		refusalBlock.SetText(refusal)
		content = append(content, refusalBlock)
	}
	toolCalls := choice.Message.ParseToolCalls()
	if legacy := choice.Message.ParseFunctionCall(); legacy != nil {
		toolCalls = append(toolCalls, dto.ToolCallRequest{ID: legacy.ID, Type: "function", Function: dto.FunctionRequest{Name: legacy.Name, Arguments: legacy.Arguments}})
	}
	for _, call := range toolCalls {
		input := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := common.Unmarshal([]byte(call.Function.Arguments), &input); err != nil || input == nil {
				common.SysError(fmt.Sprintf("invalid OpenAI tool arguments for %q", call.Function.Name))
				input = map[string]any{}
			}
		}
		content = append(content, dto.ClaudeMediaMessage{Type: "tool_use", Id: call.ID, Name: call.Function.Name, Input: input})
	}
	response.Content = content
	return response
}

func strictClaudeStopReason(reason string) string {
	switch reason {
	case "stop", "":
		return "end_turn"
	case "length", "max_tokens":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return stopReasonOpenAI2Claude(reason)
	default:
		common.SysError(fmt.Sprintf("unknown OpenAI finish reason %q", reason))
		return "end_turn"
	}
}

func stopReasonOpenAI2Claude(reason string) string {
	return reasonmap.OpenAIFinishReasonToClaudeStopReason(reason)
}

func toJSONString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func GeminiToOpenAIRequest(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	openaiRequest := &dto.GeneralOpenAIRequest{
		Model:  info.UpstreamModelName,
		Stream: lo.ToPtr(info.IsStream),
	}

	// 转换 messages
	var messages []dto.Message
	for _, content := range geminiRequest.Contents {
		message := dto.Message{
			Role: convertGeminiRoleToOpenAI(content.Role),
		}

		// 处理 parts
		var mediaContents []dto.MediaContent
		var toolCalls []dto.ToolCallRequest
		for _, part := range content.Parts {
			if part.Text != "" {
				mediaContent := dto.MediaContent{
					Type: "text",
					Text: part.Text,
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.InlineData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data),
						Detail:   "auto",
						MimeType: part.InlineData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FileData != nil {
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      part.FileData.FileUri,
						Detail:   "auto",
						MimeType: part.FileData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FunctionCall != nil {
				// 处理 Gemini 的工具调用
				toolCall := dto.ToolCallRequest{
					ID:   fmt.Sprintf("call_%d", len(toolCalls)+1), // 生成唯一ID
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      part.FunctionCall.FunctionName,
						Arguments: toJSONString(part.FunctionCall.Arguments),
					},
				}
				toolCalls = append(toolCalls, toolCall)
			} else if part.FunctionResponse != nil {
				// 处理 Gemini 的工具响应，创建单独的 tool 消息
				toolMessage := dto.Message{
					Role:       "tool",
					ToolCallId: fmt.Sprintf("call_%d", len(toolCalls)), // 使用对应的调用ID
				}
				toolMessage.SetStringContent(toJSONString(part.FunctionResponse.Response))
				messages = append(messages, toolMessage)
			}
		}

		// 设置消息内容
		if len(toolCalls) > 0 {
			// 如果有工具调用，设置工具调用
			message.SetToolCalls(toolCalls)
		} else if len(mediaContents) == 1 && mediaContents[0].Type == "text" {
			// 如果只有一个文本内容，直接设置字符串
			message.Content = mediaContents[0].Text
		} else if len(mediaContents) > 0 {
			// 如果有多个内容或包含媒体，设置为数组
			message.SetMediaContent(mediaContents)
		}

		// 只有当消息有内容或工具调用时才添加
		if len(message.ParseContent()) > 0 || len(message.ToolCalls) > 0 {
			messages = append(messages, message)
		}
	}

	openaiRequest.Messages = messages

	if geminiRequest.GenerationConfig.Temperature != nil {
		openaiRequest.Temperature = geminiRequest.GenerationConfig.Temperature
	}
	if geminiRequest.GenerationConfig.TopP != nil && *geminiRequest.GenerationConfig.TopP > 0 {
		openaiRequest.TopP = lo.ToPtr(*geminiRequest.GenerationConfig.TopP)
	}
	if geminiRequest.GenerationConfig.TopK != nil && *geminiRequest.GenerationConfig.TopK > 0 {
		openaiRequest.TopK = lo.ToPtr(int(*geminiRequest.GenerationConfig.TopK))
	}
	if geminiRequest.GenerationConfig.MaxOutputTokens != nil && *geminiRequest.GenerationConfig.MaxOutputTokens > 0 {
		openaiRequest.MaxTokens = lo.ToPtr(*geminiRequest.GenerationConfig.MaxOutputTokens)
	}
	// gemini stop sequences 最多 5 个，openai stop 最多 4 个
	if len(geminiRequest.GenerationConfig.StopSequences) > 0 {
		openaiRequest.Stop = geminiRequest.GenerationConfig.StopSequences[:4]
	}
	if geminiRequest.GenerationConfig.CandidateCount != nil && *geminiRequest.GenerationConfig.CandidateCount > 0 {
		openaiRequest.N = lo.ToPtr(*geminiRequest.GenerationConfig.CandidateCount)
	}

	// 转换工具调用
	if len(geminiRequest.GetTools()) > 0 {
		var tools []dto.ToolCallRequest
		for _, tool := range geminiRequest.GetTools() {
			if tool.FunctionDeclarations != nil {
				functionDeclarations, err := common.Any2Type[[]dto.FunctionRequest](tool.FunctionDeclarations)
				if err != nil {
					common.SysError(fmt.Sprintf("failed to parse gemini function declarations: %v (type=%T)", err, tool.FunctionDeclarations))
					continue
				}
				for _, function := range functionDeclarations {
					openAITool := dto.ToolCallRequest{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        function.Name,
							Description: function.Description,
							Parameters:  function.Parameters,
						},
					}
					tools = append(tools, openAITool)
				}
			}
		}
		if len(tools) > 0 {
			openaiRequest.Tools = tools
		}
	}

	// gemini system instructions
	if geminiRequest.SystemInstructions != nil {
		// 将系统指令作为第一条消息插入
		systemMessage := dto.Message{
			Role:    "system",
			Content: extractTextFromGeminiParts(geminiRequest.SystemInstructions.Parts),
		}
		openaiRequest.Messages = append([]dto.Message{systemMessage}, openaiRequest.Messages...)
	}

	return openaiRequest, nil
}

func convertGeminiRoleToOpenAI(geminiRole string) string {
	switch geminiRole {
	case "user":
		return "user"
	case "model":
		return "assistant"
	case "function":
		return "function"
	default:
		return "user"
	}
}

func extractTextFromGeminiParts(parts []dto.GeminiPart) string {
	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ResponseOpenAI2Gemini 将 OpenAI 响应转换为 Gemini 格式
func ResponseOpenAI2Gemini(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     openAIResponse.PromptTokens,
			CandidatesTokenCount: openAIResponse.CompletionTokens,
			TotalTokenCount:      openAIResponse.PromptTokens + openAIResponse.CompletionTokens,
		},
	}

	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}

		// 设置结束原因
		var finishReason string
		switch choice.FinishReason {
		case "stop":
			finishReason = "STOP"
		case "length":
			finishReason = "MAX_TOKENS"
		case "content_filter":
			finishReason = "SAFETY"
		case "tool_calls":
			finishReason = "STOP"
		default:
			finishReason = "STOP"
		}
		candidate.FinishReason = &finishReason

		// 转换消息内容
		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		textContent := choice.Message.StringContent()
		if textContent != "" {
			part := dto.GeminiPart{
				Text: textContent,
			}
			content.Parts = append(content.Parts, part)
		}

		toolCalls := choice.Message.ParseToolCalls()
		for _, toolCall := range toolCalls {
			var args map[string]interface{}
			if toolCall.Function.Arguments != "" {
				if err := common.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
					args = map[string]interface{}{"arguments": toolCall.Function.Arguments}
				}
			} else {
				args = make(map[string]interface{})
			}

			part := dto.GeminiPart{
				FunctionCall: &dto.FunctionCall{
					FunctionName: toolCall.Function.Name,
					Arguments:    args,
				},
			}
			content.Parts = append(content.Parts, part)
		}

		candidate.Content = content
		geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
	}

	return geminiResponse
}

// StreamResponseOpenAI2Gemini 将 OpenAI 流式响应转换为 Gemini 格式
func StreamResponseOpenAI2Gemini(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	// 检查是否有实际内容或结束标志
	hasContent := false
	hasFinishReason := false
	for _, choice := range openAIResponse.Choices {
		if len(choice.Delta.GetContentString()) > 0 || (choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0) {
			hasContent = true
		}
		if choice.FinishReason != nil {
			hasFinishReason = true
		}
	}

	// 如果没有实际内容且没有结束标志，跳过。主要针对 openai 流响应开头的空数据
	if !hasContent && !hasFinishReason {
		return nil
	}

	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     info.GetEstimatePromptTokens(),
			CandidatesTokenCount: 0, // 流式响应中可能没有完整的 usage 信息
			TotalTokenCount:      info.GetEstimatePromptTokens(),
		},
	}

	if openAIResponse.Usage != nil {
		geminiResponse.UsageMetadata.PromptTokenCount = openAIResponse.Usage.PromptTokens
		geminiResponse.UsageMetadata.CandidatesTokenCount = openAIResponse.Usage.CompletionTokens
		geminiResponse.UsageMetadata.TotalTokenCount = openAIResponse.Usage.TotalTokens
	}

	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}

		// 设置结束原因
		if choice.FinishReason != nil {
			var finishReason string
			switch *choice.FinishReason {
			case "stop":
				finishReason = "STOP"
			case "length":
				finishReason = "MAX_TOKENS"
			case "content_filter":
				finishReason = "SAFETY"
			case "tool_calls":
				finishReason = "STOP"
			default:
				finishReason = "STOP"
			}
			candidate.FinishReason = &finishReason
		}

		// 转换消息内容
		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		// 处理工具调用
		if choice.Delta.ToolCalls != nil {
			for _, toolCall := range choice.Delta.ToolCalls {
				// 解析参数
				var args map[string]interface{}
				if toolCall.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						args = map[string]interface{}{"arguments": toolCall.Function.Arguments}
					}
				} else {
					args = make(map[string]interface{})
				}

				part := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						FunctionName: toolCall.Function.Name,
						Arguments:    args,
					},
				}
				content.Parts = append(content.Parts, part)
			}
		} else {
			// 处理文本内容
			textContent := choice.Delta.GetContentString()
			if textContent != "" {
				part := dto.GeminiPart{
					Text: textContent,
				}
				content.Parts = append(content.Parts, part)
			}
		}

		candidate.Content = content
		geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
	}

	return geminiResponse
}
