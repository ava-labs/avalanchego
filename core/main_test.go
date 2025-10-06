// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

// TestMain uses goleak to verify tests in this package do not leak unexpected
// goroutines.
func TestMain(m *testing.M) {
	RegisterExtras()

	customtypes.Register()
	params.RegisterExtras()

	opts := []goleak.Option{
		// No good way to shut down these goroutines:
		goleak.IgnoreTopFunction("github.com/ava-labs/subnet-evm/core/state/snapshot.(*diskLayer).generate"),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core.(*txSenderCacher).cache"),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/metrics.(*meterArbiter).tick"),
		goleak.IgnoreTopFunction("github.com/ava-labs/avalanchego/vms/evm/metrics.(*meterArbiter).tick"),
		goleak.IgnoreTopFunction("github.com/syndtr/goleveldb/leveldb.(*DB).mpoolDrain"),
	}
	goleak.VerifyTestMain(m, opts...)
}
