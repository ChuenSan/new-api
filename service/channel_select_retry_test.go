package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// TestRetryParamResetRound guards token channel restrictions across availability-mode rounds.
func TestRetryParamResetRound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("use_channel", []string{"1", "2"})
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "group-a")
	common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, 2)
	common.SetContextKey(c, constant.ContextKeyAutoGroupRetryIndex, 3)
	common.SetContextKey(c, constant.ContextKeyModelRouteChain, modelRouteChainPacked{IDs: []int{1, 2}})
	allowedChannels := map[int]struct{}{2: {}}
	common.SetContextKey(c, constant.ContextKeyTokenAllowedChannelIds, allowedChannels)

	retry := 4
	param := &RetryParam{Ctx: c, Retry: &retry, resetNextTry: true}
	param.ResetRound()

	if got := param.GetRetry(); got != 0 {
		t.Fatalf("expected retry index 0, got %d", got)
	}
	if param.resetNextTry {
		t.Fatal("expected resetNextTry to be cleared")
	}
	if got := c.GetStringSlice("use_channel"); len(got) != 0 {
		t.Fatalf("expected used channels to be cleared, got %v", got)
	}
	if got := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); got != "" {
		t.Fatalf("expected auto group to be cleared, got %q", got)
	}
	if got := common.GetContextKeyInt(c, constant.ContextKeyAutoGroupIndex); got != 0 {
		t.Fatalf("expected auto group index 0, got %d", got)
	}
	if chain, ok := common.GetContextKey(c, constant.ContextKeyModelRouteChain); !ok || chain != nil {
		t.Fatalf("expected model route chain to be cleared, got %#v", chain)
	}
	if !IsChannelAllowedForToken(c, 2) || IsChannelAllowedForToken(c, 1) {
		t.Fatal("expected round reset to preserve the token channel restriction")
	}
}

// TestIsChannelAllowedForTokenFailsClosed distinguishes unrestricted and corrupt empty policies.
func TestIsChannelAllowedForTokenFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if !IsChannelAllowedForToken(c, 1) {
		t.Fatal("expected a missing restriction to allow every channel")
	}
	common.SetContextKey(c, constant.ContextKeyTokenAllowedChannelIds, map[int]struct{}{})
	if IsChannelAllowedForToken(c, 1) {
		t.Fatal("expected an empty restriction to deny every channel")
	}
}

// TestExcludeDisallowedEmergencyCandidates filters the complete emergency candidate set.
func TestExcludeDisallowedEmergencyCandidates(t *testing.T) {
	exclude := map[int64]struct{}{1: {}}
	candidates := []model.ResolvedRouteCandidate{
		{ChannelID: 1},
		{ChannelID: 2},
		{ChannelID: 3},
	}

	excludeDisallowedEmergencyCandidates(exclude, candidates, map[int]struct{}{3: {}})

	if _, ok := exclude[1]; !ok {
		t.Fatal("expected an existing exclusion to be preserved")
	}
	if _, ok := exclude[2]; !ok {
		t.Fatal("expected a disallowed emergency-only candidate to be excluded")
	}
	if _, ok := exclude[3]; ok {
		t.Fatal("expected an allowed emergency candidate to remain eligible")
	}
}
