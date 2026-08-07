package service

import (
	"sort"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/dto"
)

// newClaudeRelayInfo builds a minimal RelayInfo wired for OpenAI→Claude stream
// conversion, mirroring GenRelayInfoClaude without needing a gin context.
func newClaudeRelayInfo() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{}
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{
		LastMessagesType:            relaycommon.LastMessageTypeNone,
		ToolBlockIndexByOpenAIIndex: make(map[int]int),
		ToolBlockStarted:            make(map[int]bool),
	}
	return info
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// chunk is a tiny builder for one OpenAI stream chunk.
type chunk struct {
	reasoning    string
	content      string
	toolCalls    []dto.ToolCallResponse
	finishReason string
	usage        bool // emit a usage-only (no choices) chunk
}

func toolCall(index int, id, name, args string) dto.ToolCallResponse {
	return dto.ToolCallResponse{
		Index:    intPtr(index),
		ID:       id,
		Type:     "function",
		Function: dto.FunctionResponse{Name: name, Arguments: args},
	}
}

func (c chunk) toResponse() *dto.ChatCompletionsStreamResponse {
	resp := &dto.ChatCompletionsStreamResponse{Id: "msg_test", Model: "test-model", Object: "chat.completion.chunk"}
	if c.usage {
		resp.Usage = &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
		return resp
	}
	delta := dto.ChatCompletionsStreamResponseChoiceDelta{}
	if c.content != "" {
		delta.SetContentString(c.content)
	}
	if c.reasoning != "" {
		delta.ReasoningContent = strPtr(c.reasoning)
	}
	if len(c.toolCalls) > 0 {
		delta.ToolCalls = c.toolCalls
	}
	choice := dto.ChatCompletionsStreamResponseChoice{Delta: delta, Index: 0}
	if c.finishReason != "" {
		choice.FinishReason = strPtr(c.finishReason)
	}
	resp.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
	return resp
}

// blockEvent is a flattened (type, index) view of a Claude SSE event.
type blockEvent struct {
	typ   string
	index int
}

// driveStream feeds chunks through StreamResponseOpenAI2Claude and returns the
// flattened content_block event sequence plus the final Done state.
func driveStream(t *testing.T, chunks []chunk) ([]blockEvent, *relaycommon.RelayInfo) {
	t.Helper()
	info := newClaudeRelayInfo()
	var events []blockEvent
	for _, c := range chunks {
		info.SendResponseCount++
		resps := StreamResponseOpenAI2Claude(c.toResponse(), info)
		for _, r := range resps {
			switch r.Type {
			case "content_block_start", "content_block_delta", "content_block_stop":
				idx := -1
				if r.Index != nil {
					idx = *r.Index
				}
				events = append(events, blockEvent{typ: r.Type, index: idx})
			}
		}
	}
	return events, info
}

// driveStreamTrackMapping is driveStream plus a snapshot of the
// openAI-index → claude-index mapping taken at its peak (before stopOpenBlocks
// clears it at stream end).
func driveStreamTrackMapping(t *testing.T, chunks []chunk) ([]blockEvent, map[int]int) {
	t.Helper()
	info := newClaudeRelayInfo()
	var events []blockEvent
	peak := map[int]int{}
	for _, c := range chunks {
		info.SendResponseCount++
		resps := StreamResponseOpenAI2Claude(c.toResponse(), info)
		for k, v := range info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex {
			peak[k] = v
		}
		for _, r := range resps {
			switch r.Type {
			case "content_block_start", "content_block_delta", "content_block_stop":
				idx := -1
				if r.Index != nil {
					idx = *r.Index
				}
				events = append(events, blockEvent{typ: r.Type, index: idx})
			}
		}
	}
	return events, peak
}

// assertBlocksWellFormed verifies the Anthropic SSE invariants:
// every started block is stopped exactly once, every stopped block was started,
// and no delta/stop references a block outside the started set.
func assertBlocksWellFormed(t *testing.T, events []blockEvent) {
	t.Helper()
	started := map[int]bool{}
	stopped := map[int]bool{}
	var startedOrder []int
	for _, e := range events {
		switch e.typ {
		case "content_block_start":
			if started[e.index] {
				t.Errorf("duplicate content_block_start for index %d", e.index)
			}
			started[e.index] = true
			startedOrder = append(startedOrder, e.index)
		case "content_block_stop":
			if !started[e.index] {
				t.Errorf("content_block_stop for index %d that never started", e.index)
			}
			if stopped[e.index] {
				t.Errorf("duplicate content_block_stop for index %d", e.index)
			}
			stopped[e.index] = true
		case "content_block_delta":
			if !started[e.index] {
				t.Errorf("content_block_delta for index %d that never started", e.index)
			}
			if stopped[e.index] {
				t.Errorf("content_block_delta for index %d after stop", e.index)
			}
		}
	}
	for idx := range started {
		if !stopped[idx] {
			t.Errorf("block %d started but never stopped", idx)
		}
	}
}

// startedIndices returns the sorted set of indices that received a start.
func startedIndices(events []blockEvent) []int {
	set := map[int]bool{}
	for _, e := range events {
		if e.typ == "content_block_start" {
			set[e.index] = true
		}
	}
	out := make([]int, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AC1 / R7: reasoning first, then tool_calls whose upstream index starts at 1
// (the exact production failure — request ...FtA3eP9q). The Claude block index
// must stay dense {0: thinking, 1: tool_use}; no index 2, no stop on a
// never-started index.
func TestStreamResponseOpenAI2Claude_ToolIndexStartsAtOne(t *testing.T) {
	chunks := []chunk{
		{reasoning: "let me think"},
		{reasoning: " more"},
		{toolCalls: []dto.ToolCallResponse{toolCall(1, "toolu_1", "get_weather", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(1, "", "", `{"city"`)}},
		{toolCalls: []dto.ToolCallResponse{toolCall(1, "", "", `:"北京"}`)}},
		{finishReason: "tool_calls"},
		{usage: true},
	}
	events, info := driveStream(t, chunks)
	assertBlocksWellFormed(t, events)
	if got := startedIndices(events); !equalInts(got, []int{0, 1}) {
		t.Fatalf("started indices = %v, want [0 1] (dense, no hole)", got)
	}
	if !info.ClaudeConvertInfo.Done {
		t.Fatalf("stream not marked Done")
	}
}

// AC2: text first, then three parallel tools with contiguous upstream indices
// 0,1,2 → dense Claude indices 1,2,3, each paired start/stop.
func TestStreamResponseOpenAI2Claude_ParallelToolsAfterText(t *testing.T) {
	chunks := []chunk{
		{content: "checking tools"},
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "a", "f0", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(1, "b", "f1", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(2, "c", "f2", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "", "", `{}`), toolCall(1, "", "", `{}`), toolCall(2, "", "", `{}`)}},
		{finishReason: "tool_calls"},
		{usage: true},
	}
	events, _ := driveStream(t, chunks)
	assertBlocksWellFormed(t, events)
	if got := startedIndices(events); !equalInts(got, []int{0, 1, 2, 3}) {
		t.Fatalf("started indices = %v, want [0 1 2 3]", got)
	}
}

// AC3: out-of-order upstream indices (2 before 0) — args for a given upstream
// index must always land on the same Claude block, and Claude indices stay dense.
func TestStreamResponseOpenAI2Claude_OutOfOrderToolIndex(t *testing.T) {
	chunks := []chunk{
		{toolCalls: []dto.ToolCallResponse{toolCall(2, "c", "f2", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "a", "f0", "")}},
		{toolCalls: []dto.ToolCallResponse{toolCall(2, "", "", `{"x":1}`)}},
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "", "", `{"y":2}`)}},
		{finishReason: "tool_calls"},
		{usage: true},
	}
	events, mapping := driveStreamTrackMapping(t, chunks)
	assertBlocksWellFormed(t, events)
	if got := startedIndices(events); !equalInts(got, []int{0, 1}) {
		t.Fatalf("started indices = %v, want [0 1] (dense despite upstream 2-first)", got)
	}
	// upstream index 2 → first allocated Claude block 0; upstream index 0 → block 1.
	if mapping[2] != 0 {
		t.Errorf("openAI index 2 mapped to %d, want 0", mapping[2])
	}
	if mapping[0] != 1 {
		t.Errorf("openAI index 0 mapped to %d, want 1", mapping[0])
	}
}

// AC4: the very first chunk is already a tool call (SendResponseCount==1
// branch) — the tool_use block must be allocated through the same dense path.
func TestStreamResponseOpenAI2Claude_FirstChunkIsToolCall(t *testing.T) {
	chunks := []chunk{
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "a", "f0", `{"q"`)}},
		{toolCalls: []dto.ToolCallResponse{toolCall(0, "", "", `:"x"}`)}},
		{finishReason: "tool_calls"},
		{usage: true},
	}
	events, _ := driveStream(t, chunks)
	assertBlocksWellFormed(t, events)
	if got := startedIndices(events); !equalInts(got, []int{0}) {
		t.Fatalf("started indices = %v, want [0]", got)
	}
}

// Regression: pure text stream (no tools) still emits a single dense block.
func TestStreamResponseOpenAI2Claude_PureText(t *testing.T) {
	chunks := []chunk{
		{content: "hello"},
		{content: " world"},
		{finishReason: "stop"},
		{usage: true},
	}
	events, _ := driveStream(t, chunks)
	assertBlocksWellFormed(t, events)
	if got := startedIndices(events); !equalInts(got, []int{0}) {
		t.Fatalf("started indices = %v, want [0]", got)
	}
}
