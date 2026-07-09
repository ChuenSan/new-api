package circuitbreaker

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock replaces the package-level now so tests advance time deterministically.
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }
func (f *fakeClock) advance(d time.Duration) {
	// advance in steps under ProbeTimeout so a probe-in-flight check degrades
	// correctly; callers control exact elapsed time.
	f.t = f.t.Add(d)
}

func setupTest(t *testing.T) *fakeClock {
	t.Helper()
	reset()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	prev := now
	now = clock.now
	t.Cleanup(func() { now = prev })
	return clock
}

func ptrInt(v int) *int { return &v }

func TestClosedToOpenAfterConsecutiveFailures(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 1, "gpt-4"
	threshold := operationDefaultThreshold()

	// threshold-1 failures stay CLOSED.
	for i := 0; i < threshold-1; i++ {
		RecordFailure(ch, model, 500, nil)
	}
	assert.False(t, IsOpen(ch, model))
	assert.False(t, IsHalfOpen(ch, model))

	// One more trips to OPEN.
	RecordFailure(ch, model, 500, nil)
	assert.True(t, IsOpen(ch, model))
	assert.False(t, IsHalfOpen(ch, model))
	// Just past threshold should not yet recover (cooldown not elapsed).
	clock.advance(time.Second)
	assert.True(t, IsOpen(ch, model))
}

func TestSuccessResetsClosedCounter(t *testing.T) {
	setupTest(t)
	const ch, model = 2, "claude-3"
	threshold := operationDefaultThreshold()

	for i := 0; i < threshold-1; i++ {
		RecordFailure(ch, model, 429, nil)
	}
	// A success in CLOSED clears the partial failures.
	RecordSuccess(ch, model)
	assert.False(t, IsOpen(ch, model))

	// Now threshold failures are needed again, not just one.
	for i := 0; i < threshold-1; i++ {
		RecordFailure(ch, model, 500, nil)
	}
	assert.False(t, IsOpen(ch, model))
	RecordFailure(ch, model, 500, nil)
	assert.True(t, IsOpen(ch, model))
}

func Test2xxFailureTreatedAsSuccess(t *testing.T) {
	setupTest(t)
	const ch, model = 3, "gemini-pro"
	threshold := operationDefaultThreshold()

	for i := 0; i < threshold-1; i++ {
		RecordFailure(ch, model, 500, nil)
	}
	// A 2xx through RecordFailure resets (parity with success).
	RecordFailure(ch, model, 200, nil)
	assert.False(t, IsOpen(ch, model))

	for i := 0; i < threshold-1; i++ {
		RecordFailure(ch, model, 500, nil)
	}
	assert.False(t, IsOpen(ch, model))
}

func TestOpenTransitionsToHalfOpenAfterCooldown(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 4, "gpt-3.5"
	cooldown := operationDefaultCooldown()
	trip(t, ch, model, nil)

	// Before cooldown: still OPEN, IsOpen true.
	clock.advance(time.Duration(cooldown-1) * time.Second)
	assert.True(t, IsOpen(ch, model))
	assert.False(t, IsHalfOpen(ch, model))

	// At/after cooldown: IsOpen lazily flips to HALF_OPEN and returns false.
	clock.advance(2 * time.Second)
	assert.False(t, IsOpen(ch, model))
	assert.True(t, IsHalfOpen(ch, model))
}

func TestHalfOpenProbeSuccessClosesCircuit(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 5, "qwen"
	trip(t, ch, model, nil)
	clock.advance(time.Duration(operationDefaultCooldown()) * time.Second)
	require.False(t, IsOpen(ch, model))
	require.True(t, IsHalfOpen(ch, model))

	require.True(t, AllowProbe(ch, model))
	RecordSuccess(ch, model)

	assert.False(t, IsOpen(ch, model))
	assert.False(t, IsHalfOpen(ch, model))
	// Entry fully cleared: a fresh failure starts from zero count.
	assert.False(t, IsOpen(ch, model))
}

func TestHalfOpenOnlyOneProbeAllowed(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 6, "llama"
	trip(t, ch, model, nil)
	clock.advance(time.Duration(operationDefaultCooldown()) * time.Second)
	require.False(t, IsOpen(ch, model), "expected entry to leave OPEN after cooldown")
	require.True(t, IsHalfOpen(ch, model))

	assert.True(t, AllowProbe(ch, model))
	// Second concurrent claim is refused while the probe is in flight.
	assert.False(t, AllowProbe(ch, model))
}

func TestHalfOpenProbeFailureReopensWithFreshCooldown(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 7, "mistral"
	trip(t, ch, model, nil)
	clock.advance(time.Duration(operationDefaultCooldown()) * time.Second)
	require.False(t, IsOpen(ch, model), "expected entry to leave OPEN after cooldown")
	require.True(t, AllowProbe(ch, model))

	RecordFailure(ch, model, 500, nil)
	// Immediately OPEN again, with a fresh cooldown window.
	assert.True(t, IsOpen(ch, model))
	clock.advance(time.Duration(operationDefaultCooldown()-1) * time.Second)
	assert.True(t, IsOpen(ch, model))
	clock.advance(2 * time.Second)
	assert.False(t, IsOpen(ch, model))
	assert.True(t, IsHalfOpen(ch, model))
}

func TestPerChannelOverrideThresholdAndCooldown(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 8, "yi"
	s := &dto.ChannelOtherSettings{
		CircuitBreakerFailureThreshold: ptrInt(2),
		CircuitBreakerCooldownSeconds:  ptrInt(10),
	}

	// threshold 2 (override) trips faster than the global 3.
	RecordFailure(ch, model, 500, s)
	assert.False(t, IsOpen(ch, model))
	RecordFailure(ch, model, 500, s)
	assert.True(t, IsOpen(ch, model))

	// cooldown 10s (override) gates the HALF_OPEN transition.
	clock.advance(9 * time.Second)
	assert.True(t, IsOpen(ch, model))
	clock.advance(2 * time.Second)
	assert.False(t, IsOpen(ch, model))
	assert.True(t, IsHalfOpen(ch, model))
}

func TestNonPositiveOverrideFallsBackToGlobal(t *testing.T) {
	setupTest(t)
	const ch, model = 9, "baichuan"
	s := &dto.ChannelOtherSettings{
		CircuitBreakerFailureThreshold: ptrInt(0),
		CircuitBreakerCooldownSeconds:  ptrInt(-5),
	}
	assert.Equal(t, operationDefaultThreshold(), GetFailureThreshold(s))
	assert.Equal(t, operationDefaultCooldown(), GetCooldownSeconds(s))
}

func TestNilSettingsUsesGlobalDefault(t *testing.T) {
	setupTest(t)
	assert.Equal(t, operationDefaultThreshold(), GetFailureThreshold(nil))
	assert.Equal(t, operationDefaultCooldown(), GetCooldownSeconds(nil))
	assert.Equal(t, operationDefaultThreshold(), GetFailureThreshold(&dto.ChannelOtherSettings{}))
}

func TestLeakedProbeRecycledAfterTimeout(t *testing.T) {
	clock := setupTest(t)
	const ch, model = 10, "deepseek"
	trip(t, ch, model, nil)
	clock.advance(time.Duration(operationDefaultCooldown()) * time.Second)
	require.False(t, IsOpen(ch, model), "expected entry to leave OPEN after cooldown")
	require.True(t, AllowProbe(ch, model))
	// Probe never reports back. Before ProbeTimeout it stays claimed.
	clock.advance(ProbeTimeout - time.Second)
	assert.False(t, AllowProbe(ch, model))
	// After ProbeTimeout the slot is recycled for a fresh probe.
	clock.advance(2 * time.Second)
	assert.True(t, AllowProbe(ch, model))
}

func TestIsolatedPerChannelModelKeys(t *testing.T) {
	setupTest(t)
	// Same channel, different models are independent.
	trip(t, 11, "a", nil)
	assert.True(t, IsOpen(11, "a"))
	assert.False(t, IsOpen(11, "b"))
	// Same model, different channels are independent.
	trip(t, 12, "a", nil)
	assert.True(t, IsOpen(12, "a"))
	assert.False(t, IsOpen(13, "a"))
}

// operationDefaultThreshold / cooldown read the live global default so the
// test tracks any future change to operation_setting defaults.
func operationDefaultThreshold() int { return GetFailureThreshold(nil) }
func operationDefaultCooldown() int  { return GetCooldownSeconds(nil) }

// trip drives a fresh entry to OPEN using the global default threshold.
func trip(t *testing.T, ch int, model string, s *dto.ChannelOtherSettings) {
	t.Helper()
	threshold := operationDefaultThreshold()
	if s != nil && s.CircuitBreakerFailureThreshold != nil && *s.CircuitBreakerFailureThreshold > 0 {
		threshold = *s.CircuitBreakerFailureThreshold
	}
	for i := 0; i < threshold; i++ {
		RecordFailure(ch, model, 500, s)
	}
	require.True(t, IsOpen(ch, model), "expected channel to trip to OPEN")
}
