package modelroute

import (
	"github.com/QuantumNous/new-api/model"
)

// AttemptOutcome is the classified result of one upstream try (PRD §11.1 / §30).
type AttemptOutcome struct {
	Success             bool
	HasEmittedUserBytes bool
	StatusCode          int
	RetryAfterSec       int
	StreamInterrupted   bool
	ErrorClass          model.ErrorClass
	Event               TransitionEvent
}

// normalizeAttemptOutcome enforces the production success contract even when
// a caller constructs AttemptOutcome directly instead of using ClassifyAttempt.
func normalizeAttemptOutcome(out AttemptOutcome) AttemptOutcome {
	if isSuccessfulProductionResult(out.Success, out.StatusCode, out.StreamInterrupted) {
		out.Success = true
		out.ErrorClass = ""
		out.Event = EventProductionSuccess
		return out
	}

	out.Success = false
	// Production failures are always normalized from the raw outcome so a
	// stale or caller-supplied success/manual event cannot bypass the breaker.
	class, event := classifyProductionFailure(out.StatusCode)
	if out.StreamInterrupted {
		// A stream interruption is transport failure regardless of status.
		class, event = model.ErrorTemporary, EventTemporaryFail
	}
	out.ErrorClass = class
	out.Event = event
	return out
}

// ClassifyAttempt maps raw attempt signals into state-machine event (PRD §11.1 / §24 / §25).
func ClassifyAttempt(success bool, statusCode int, hasEmittedUserBytes bool, streamInterrupted bool) AttemptOutcome {
	return normalizeAttemptOutcome(AttemptOutcome{
		Success:             success,
		HasEmittedUserBytes: hasEmittedUserBytes,
		StatusCode:          statusCode,
		StreamInterrupted:   streamInterrupted,
	})
}

// ApplyAttemptOutcome updates metrics/role after one try.
// Returns canTransparentRetry: true only when failure is pre-first-byte (PRD §11.1).
func ApplyAttemptOutcome(c *model.ResolvedRouteCandidate, out AttemptOutcome) (canTransparentRetry bool) {
	if c == nil || c.Metrics == nil {
		return false
	}
	mk := MakeMetricsKey(c.ChannelID, c.EffectiveModel)
	lock := metricsLockFor(mk)
	lock.Lock()
	defer lock.Unlock()
	c.Metrics = refreshMetricsLocked(c.Metrics)
	out = normalizeAttemptOutcome(out)
	if out.Success {
		applyTransitionLocked(c.Metrics, EventProductionSuccess, 0)
		// successful production validation → PRIMARY (BOOTSTRAP or first healthy)
		role := GlobalRoles.Get(mk)
		if role == model.RoleBootstrap || role == model.RoleNone {
			GlobalRoles.Set(mk, model.RolePrimary)
		}
		return false
	}

	if out.StreamInterrupted {
		RecordStreamInterrupted(c.Metrics)
	}
	if out.Event != "" {
		applyTransitionLocked(c.Metrics, out.Event, out.RetryAfterSec)
	}
	if out.HasEmittedUserBytes {
		// post-first-byte failure, including interruption, cannot be replayed.
		return false
	}

	// pre-first-byte failure → update state then transparent retry next
	return true
}

// RecordStreamInterrupted bumps stream interruption counter/EMA placeholder (PRD §11.1 / §20).
// Full EMA update lands in P6; here we only sample-count + set temporary class marker.
func RecordStreamInterrupted(m *model.ChannelModelMetrics) {
	if m == nil {
		return
	}
	m.ProductionSampleCount++
	// soft mark: increase stream interruption ema toward 1 with default alpha when nil→0
	prev := 0.0
	if m.StreamInterruptionEMA != nil {
		prev = *m.StreamInterruptionEMA
	}
	alpha := model.DefaultStreamInterruptionEMAAlpha
	v := alpha*1.0 + (1-alpha)*prev
	m.StreamInterruptionEMA = &v
	ts := now().Unix()
	m.LastFailureAt = &ts
	GlobalMetricsRuntime.Put(m)
}

// TransparentRetryPlan walks candidates with pre-first-byte failure retry (PRD §11.1).
// tryFn returns AttemptOutcome for one candidate; stops on success or post-byte failure.
type TransparentRetryPlan struct {
	Candidates []model.ResolvedRouteCandidate
	ColdStart  bool
}

// RunTransparentRetry executes tryFn across candidates with transparent retry rules.
// Returns the successful candidate index, outcome, and whether all failed pre-byte.
func RunTransparentRetry(
	plan TransparentRetryPlan,
	tryFn func(c model.ResolvedRouteCandidate, index int) AttemptOutcome,
) (successIdx int, last AttemptOutcome, exhausted bool) {
	successIdx = -1
	for i, c := range plan.Candidates {
		out := normalizeAttemptOutcome(tryFn(c, i))
		retry := ApplyAttemptOutcome(&plan.Candidates[i], out)
		last = out
		if out.Success {
			return i, out, false
		}
		if !retry {
			// post-byte failure: stop chain for this request
			return -1, out, false
		}
		// pre-byte: continue
	}
	return -1, last, true
}
