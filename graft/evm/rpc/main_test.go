// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	cchainCustomTypes "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	subnetCustomTypes "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

// TestMain runs all tests in this package with proper EVM type registration.
// By default, tests are run TWICE - once with C-Chain types and once with Subnet-EVM types.
//
// The EVM_VARIANT environment variable can override this behavior:
//   - "cchain": Run tests only with C-Chain type registration
//   - "subnetEVM": Run tests only with Subnet-EVM type registration
//   - "both" or unset: Run tests twice with both variants (default)
//
// Note: Because this package is imported by coreth and subnet-evm, we cannot use
// the emulate package here as it would create a circular dependency. Instead, we
// directly call the Register() functions.
func TestMain(m *testing.M) {
	variant := os.Getenv("EVM_VARIANT")
	if variant == "" {
		variant = "both"
	}

	var exitCode int

	switch variant {
	case "cchain":
		fmt.Println("Running RPC tests with C-Chain type registration")
		cchainCustomTypes.Register()
		exitCode = m.Run()
	case "subnetEVM":
		fmt.Println("Running RPC tests with Subnet-EVM type registration")
		subnetCustomTypes.Register()
		exitCode = m.Run()
	case "both":
		// We cannot call Register() twice in the same process, so we run
		// tests in separate processes by re-executing with different environment variables.

		fmt.Println("=== Running RPC tests with both C-Chain and Subnet-EVM ===")

		// Run C-Chain tests in a separate process
		fmt.Println("--- Running C-Chain variant ---")
		cchainCmd := exec.Command("go", "test", "-v")
		cchainCmd.Env = append(os.Environ(), "EVM_VARIANT=cchain")
		cchainCmd.Stdout = os.Stdout
		cchainCmd.Stderr = os.Stderr
		cchainErr := cchainCmd.Run()

		// Run Subnet-EVM tests in a separate process
		fmt.Println("\n--- Running Subnet-EVM variant ---")
		subnetCmd := exec.Command("go", "test", "-v")
		subnetCmd.Env = append(os.Environ(), "EVM_VARIANT=subnetEVM")
		subnetCmd.Stdout = os.Stdout
		subnetCmd.Stderr = os.Stderr
		subnetErr := subnetCmd.Run()

		if cchainErr != nil || subnetErr != nil {
			if cchainErr != nil {
				fmt.Fprintf(os.Stderr, "\nC-Chain tests failed: %v\n", cchainErr)
			}
			if subnetErr != nil {
				fmt.Fprintf(os.Stderr, "Subnet-EVM tests failed: %v\n", subnetErr)
			}
			exitCode = 1
		} else {
			fmt.Println("\nAll tests passed with both variants")
			exitCode = 0
		}
	default:
		fmt.Fprintf(os.Stderr, "Invalid EVM_VARIANT: %s (must be 'cchain', 'subnetEVM', or 'both')\n", variant)
		exitCode = 1
	}

	os.Exit(exitCode)
}
