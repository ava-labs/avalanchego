// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpc_test contains TestMain for the rpc package.
// By using an external test package, we can import the emulate package
// without creating a circular dependency (since rpc_test is not part of rpc).
package rpc_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/emulate"
)

// TestMain runs all tests in the rpc package with proper EVM type registration.
// By default, tests are run TWICE - once with C-Chain types and once with Subnet-EVM types.
//
// The EVM_VARIANT environment variable can override this behavior:
//   - "cchain": Run tests only with C-Chain type registration
//   - "subnetEVM": Run tests only with Subnet-EVM type registration
//   - "both" or unset: Run tests twice with both variants (default)
//
// This uses the emulate package's temporary registration mechanism, which properly
// cleans up after each test run, allowing us to test both variants in sequence.
func TestMain(m *testing.M) {
	variant := os.Getenv("EVM_VARIANT")
	if variant == "" {
		variant = "both"
	}

	var exitCode int

	switch variant {
	case "cchain":
		fmt.Println("=== Running RPC tests with C-Chain type registration ===")
		exitCode = runWith(m, emulate.CChain)
	case "subnetEVM":
		fmt.Println("=== Running RPC tests with Subnet-EVM type registration ===")
		exitCode = runWith(m, emulate.SubnetEVM)
	case "both":
		fmt.Println("=== Running RPC tests with both C-Chain and Subnet-EVM ===")

		fmt.Println("--- Running C-Chain variant ---")
		cchainCode := runWith(m, emulate.CChain)

		fmt.Println("\n--- Running Subnet-EVM variant ---")
		subnetCode := runWith(m, emulate.SubnetEVM)

		if cchainCode != 0 {
			fmt.Fprintf(os.Stderr, "\n✗ C-Chain tests failed with exit code %d\n", cchainCode)
			exitCode = cchainCode
		} else if subnetCode != 0 {
			fmt.Fprintf(os.Stderr, "\n✗ Subnet-EVM tests failed with exit code %d\n", subnetCode)
			exitCode = subnetCode
		} else {
			fmt.Println("\n✓ All tests passed with both variants")
			exitCode = 0
		}
	default:
		fmt.Fprintf(os.Stderr, "Invalid EVM_VARIANT: %s (must be 'cchain', 'subnetEVM', or 'both')\n", variant)
		exitCode = 1
	}

	os.Exit(exitCode)
}

// runWith executes tests with the specified emulation function.
func runWith(m *testing.M, emulateFn func(func() error) error) int {
	var code int
	_ = emulateFn(func() error {
		code = m.Run()
		return nil
	})
	return code
}
