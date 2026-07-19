package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func TestSetupContextForTokenAvailabilityMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	if err := SetupContextForToken(c, &model.Token{AvailabilityMode: true}); err != nil {
		t.Fatalf("failed to set token context: %v", err)
	}
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenAvailabilityMode) {
		t.Fatal("expected availability mode in request context")
	}
	if !IsTokenAvailabilityMode(c) {
		t.Fatal("expected HTTP relay request to use availability mode")
	}
}

func TestTokenAvailabilityModeExcludesRealtime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	common.SetContextKey(c, constant.ContextKeyTokenAvailabilityMode, true)

	if IsTokenAvailabilityMode(c) {
		t.Fatal("expected realtime request to keep existing behavior")
	}
}

// TestSetupContextForTokenAllowedChannelsPreservesEmptyRestriction guards corrupt-policy fail-closed behavior.
func TestSetupContextForTokenAllowedChannelsPreservesEmptyRestriction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	if err := SetupContextForToken(c, &model.Token{AllowedChannelIds: model.ChannelIDList{}}); err != nil {
		t.Fatalf("failed to set token context: %v", err)
	}
	value, exists := common.GetContextKey(c, constant.ContextKeyTokenAllowedChannelIds)
	if !exists {
		t.Fatal("expected empty channel restriction to remain present")
	}
	allowed, ok := value.(map[int]struct{})
	if !ok || len(allowed) != 0 {
		t.Fatalf("expected an empty allowed-channel set, got %#v", value)
	}
}
