package modelroute

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
)

const metricsLockStripeCount = 256

var metricsLockStripes [metricsLockStripeCount]sync.Mutex

func metricsLockFor(mk model.MetricsKey) *sync.Mutex {
	hash := uint32(2166136261)
	for _, b := range []byte(mk.String()) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return &metricsLockStripes[hash%metricsLockStripeCount]
}
