package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

func TestRetryParamResetRound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("use_channel", []string{"1", "2"})
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "group-a")
	common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, 2)
	common.SetContextKey(c, constant.ContextKeyAutoGroupRetryIndex, 3)
	common.SetContextKey(c, constant.ContextKeyModelRouteChain, modelRouteChainPacked{IDs: []int{1, 2}})

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
}
