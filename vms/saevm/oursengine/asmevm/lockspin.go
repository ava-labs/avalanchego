package asmevm

import (
	"sync"
	"time"
)

// lockSpinFor is how long lockSpin tries before it parks.
var lockSpinFor = 50 * time.Microsecond

// lockSpin locks mu for the committer. Its goroutine is locked to a thread pinned to a core of
// its own (pipeline.pinExecutor), so parking on a held mutex parks that thread and waking it
// takes two thread handoffs (~150 μs measured); the workers hold Speculator.mu for a few μs at a
// time. lockSpin retries for up to lockSpinFor before it parks: busy waiting costs no other
// goroutine anything on that core.
func lockSpin(mu *sync.Mutex) {
	if mu.TryLock() {
		return
	}
	t := time.Now()
	for time.Since(t) < lockSpinFor {
		for range 64 { // a few hundred ns between tries
			if mu.TryLock() {
				return
			}
		}
	}
	mu.Lock()
}
