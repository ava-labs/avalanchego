// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc_test

import (
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/graft/evm/libevmtest"
)

// TestMain runs all tests in the rpc package with proper EVM type registration.
// Tests are run twice - once with C-Chain types and once with Subnet-EVM types.
// Both test runs must pass for the overall result to be successful.
func TestMain(m *testing.M) {
	os.Exit(libevmtest.RunWithAll(m))
}
