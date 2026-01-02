// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
//
// This uses the libevmtest package's helper which:
// - Registers types using the emulate package's temporary registration mechanism
// - Properly cleans up after each test run
// - Allows tests to check which variant is active via libevmtest.CurrentVariant()
func TestMain(m *testing.M) {
	os.Exit(libevmtest.RunWithAll(m))
}
