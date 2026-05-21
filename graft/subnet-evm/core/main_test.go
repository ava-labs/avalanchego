// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"testing"

	"github.com/MetalBlockchain/libevm/log"
	"go.uber.org/goleak"

	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/params"
	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/plugin/evm/customtypes"
)

// TestMain uses goleak to verify tests in this package do not leak unexpected
// goroutines.
func TestMain(m *testing.M) {
	RegisterExtras()

	customtypes.Register()
	params.RegisterExtras()

	// May of these tests are likely to fail due to `log.Crit` in goroutines.
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelCrit, true)))

	opts := []goleak.Option{
		// No good way to shut down these goroutines:
		goleak.IgnoreTopFunction("github.com/MetalBlockchain/libevm/core.(*txSenderCacher).cache"),
		goleak.IgnoreTopFunction("github.com/MetalBlockchain/libevm/metrics.(*meterArbiter).tick"),
		goleak.IgnoreTopFunction("github.com/MetalBlockchain/metalgo/vms/evm/metrics.(*meterArbiter).tick"),
		goleak.IgnoreTopFunction("github.com/syndtr/goleveldb/leveldb.(*DB).mpoolDrain"),
	}
	goleak.VerifyTestMain(m, opts...)
}
