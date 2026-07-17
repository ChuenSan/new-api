package controller

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRequestIsActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)

	if !requestIsActive(c) {
		t.Fatal("expected request to be active")
	}
	cancel()
	if requestIsActive(c) {
		t.Fatal("expected canceled request to be inactive")
	}
}

func TestWaitForAvailabilityRetryStopsOnCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	cancel()

	started := time.Now()
	if waitForAvailabilityRetry(c) {
		t.Fatal("expected canceled request not to retry")
	}
	if elapsed := time.Since(started); elapsed >= availabilityNoChannelRetryDelay {
		t.Fatalf("expected cancellation before retry delay, elapsed %s", elapsed)
	}
}

func TestAvailabilityRoundsHaveNoCountLimit(t *testing.T) {
	for round := 0; round < 10_000; round++ {
		if !shouldStartAvailabilityRound(true, true, false, true) {
			t.Fatalf("expected availability retry to continue at round %d", round)
		}
	}
}

func TestAvailabilityRoundHardStops(t *testing.T) {
	tests := []struct {
		name             string
		enabled          bool
		retryableFailure bool
		responseStarted  bool
		requestActive    bool
	}{
		{name: "disabled", retryableFailure: true, requestActive: true},
		{name: "non-retryable", enabled: true, requestActive: true},
		{name: "response-started", enabled: true, retryableFailure: true, responseStarted: true, requestActive: true},
		{name: "request-canceled", enabled: true, retryableFailure: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if shouldStartAvailabilityRound(tt.enabled, tt.retryableFailure, tt.responseStarted, tt.requestActive) {
				t.Fatal("expected availability retry to stop")
			}
		})
	}
}
