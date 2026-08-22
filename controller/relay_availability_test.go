package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// TestRelayProductionCompletedNormally guards the clean HTTP 200 completion predicate's stream cases.
func TestRelayProductionCompletedNormally(t *testing.T) {
	done := relaycommon.NewStreamStatus()
	done.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	eofWithError := relaycommon.NewStreamStatus()
	eofWithError.SetEndReason(relaycommon.StreamEndReasonEOF, errors.New("stream failed"))
	softError := relaycommon.NewStreamStatus()
	softError.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	softError.RecordError("invalid chunk")

	tests := []struct {
		name string
		info *relaycommon.RelayInfo
		want bool
	}{
		{name: "nil", info: nil, want: false},
		{name: "non-stream", info: &relaycommon.RelayInfo{}, want: true},
		{name: "stream without status", info: &relaycommon.RelayInfo{IsStream: true}, want: false},
		{name: "stream done", info: &relaycommon.RelayInfo{IsStream: true, StreamStatus: done}, want: true},
		{name: "stream eof with end error", info: &relaycommon.RelayInfo{IsStream: true, StreamStatus: eofWithError}, want: false},
		{name: "stream soft error", info: &relaycommon.RelayInfo{IsStream: true, StreamStatus: softError}, want: false},
		{name: "stream timeout", info: &relaycommon.RelayInfo{
			IsStream:     true,
			StreamStatus: &relaycommon.StreamStatus{EndReason: relaycommon.StreamEndReasonTimeout},
		}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relayProductionCompletedNormally(tt.info); got != tt.want {
				t.Fatalf("relayProductionCompletedNormally() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
