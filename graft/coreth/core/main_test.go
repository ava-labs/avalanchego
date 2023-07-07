// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain uses goleak to verify tests in this package do not leak unexpected
// goroutines.
func TestMain(m *testing.M) {
	opts := []goleak.Option{
		// No good way to shut down these goroutines:
		goleak.IgnoreTopFunction("github.com/ava-labs/coreth/core/state/snapshot.(*diskLayer).generate"),
		goleak.IgnoreTopFunction("github.com/ava-labs/coreth/metrics.(*meterArbiter).tick"),
		goleak.IgnoreTopFunction("github.com/syndtr/goleveldb/leveldb.(*DB).mpoolDrain"),
	}
	goleak.VerifyTestMain(m, opts...)
}
