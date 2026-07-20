package modelroute

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// CalibrationPersister snapshots runtime metrics to DB (PRD §17).
type CalibrationPersister struct {
	mu       sync.Mutex
	lastSnap time.Time
	// dirty marks keys needing snapshot
	dirty map[string]struct{}
	stop  chan struct{}
	wg    sync.WaitGroup
}

// GlobalCalibrationPersister is process-local snapshot coordinator.
var GlobalCalibrationPersister = &CalibrationPersister{
	dirty: make(map[string]struct{}),
	stop:  make(chan struct{}),
}

// MarkDirty queues a metrics key for next snapshot.
func (p *CalibrationPersister) MarkDirty(mk model.MetricsKey) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dirty == nil {
		p.dirty = make(map[string]struct{})
	}
	p.dirty[mk.String()] = struct{}{}
}

func (p *CalibrationPersister) ClearDirty(mk model.MetricsKey) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.dirty, mk.String())
}

// SnapshotNow flushes all dirty (or all runtime) metrics to DB (PRD §17 critical / exit).
func (p *CalibrationPersister) SnapshotNow() (int, error) {
	if p == nil {
		return 0, nil
	}
	p.mu.Lock()
	keys := make([]string, 0, len(p.dirty))
	for k := range p.dirty {
		keys = append(keys, k)
	}
	p.dirty = make(map[string]struct{})
	p.lastSnap = now()
	p.mu.Unlock()

	var candidates []*model.ChannelModelMetrics
	GlobalMetricsRuntime.mu.RLock()
	if len(keys) == 0 {
		for _, m := range GlobalMetricsRuntime.data {
			if m != nil {
				candidates = append(candidates, m)
			}
		}
	} else {
		for _, k := range keys {
			if m, ok := GlobalMetricsRuntime.data[k]; ok && m != nil {
				candidates = append(candidates, m)
			}
		}
	}
	GlobalMetricsRuntime.mu.RUnlock()

	count := 0
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		mk := candidate.MetricsKey()
		if _, ok := seen[mk.String()]; ok {
			continue
		}
		seen[mk.String()] = struct{}{}
		lock := metricsLockFor(mk)
		lock.Lock()
		current := GlobalMetricsRuntime.Get(mk)
		if current == nil {
			lock.Unlock()
			continue
		}
		err := model.UpsertChannelModelMetrics(current)
		lock.Unlock()
		if err != nil {
			p.MarkDirty(mk)
			return count, err
		}
		count++
	}
	return count, nil
}

// SnapshotCritical immediately persists one metrics row after critical state change (PRD §17).
func (p *CalibrationPersister) SnapshotCritical(m *model.ChannelModelMetrics) error {
	if m == nil {
		return nil
	}
	lock := metricsLockFor(m.MetricsKey())
	lock.Lock()
	defer lock.Unlock()
	return p.snapshotCriticalLocked(m)
}

func (p *CalibrationPersister) snapshotCriticalLocked(m *model.ChannelModelMetrics) error {
	if p == nil || m == nil {
		return nil
	}
	current := GlobalMetricsRuntime.Get(m.MetricsKey())
	if current == nil {
		return nil
	}
	if err := model.UpsertChannelModelMetrics(current); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.dirty, current.MetricsKey().String())
	p.mu.Unlock()
	return nil
}

// StartPeriodicSnapshot runs 30–60s interval snapshots (PRD §17 / §33 default 60s).
func (p *CalibrationPersister) StartPeriodicSnapshot(interval time.Duration) {
	if p == nil {
		return
	}
	if interval <= 0 {
		interval = time.Duration(model.DefaultCalibrationSnapshotIntervalSec) * time.Second
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				_, _ = p.SnapshotNow()
			}
		}
	}()
}

// StopPeriodicSnapshot stops the background loop.
func (p *CalibrationPersister) StopPeriodicSnapshot() {
	if p == nil {
		return
	}
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	p.wg.Wait()
	p.stop = make(chan struct{})
}
