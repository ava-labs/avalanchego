// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevmtest provides test utilities for packages that need to test
// both C-Chain and Subnet-EVM type registration.
package libevmtest

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/emulate"
)

// Variant represents which EVM variant is currently registered.
type Variant int32

const (
	// UnknownVariant indicates no variant is registered.
	UnknownVariant Variant = iota
	// CChainVariant indicates C-Chain types are registered.
	CChainVariant
	// SubnetEVMVariant indicates Subnet-EVM types are registered.
	SubnetEVMVariant
)

// currentVariant tracks which EVM variant is currently registered.
// This allows tests to check which variant they're running under.
var currentVariant atomic.Int32

// CurrentVariant returns which EVM variant is currently registered.
// This is safe to call from tests running in parallel.
func CurrentVariant() Variant {
	return Variant(currentVariant.Load())
}

// RunWithAll runs all tests with both C-Chain and Subnet-EVM type registration.
// Tests are run twice - once with C-Chain types and once with Subnet-EVM types.
// Both test runs must pass for the overall result to be successful.
//
// This function sets a global variable that tests can check via CurrentVariant()
// to determine which variant is currently registered.
func RunWithAll(m *testing.M) int {
	fmt.Println("=== Running tests with both C-Chain and Subnet-EVM ===")

	fmt.Println("\n--- Running C-Chain variant ---")
	cchainCode := runWith(m, emulate.CChain, CChainVariant)

	fmt.Println("\n--- Running Subnet-EVM variant ---")
	subnetCode := runWith(m, emulate.SubnetEVM, SubnetEVMVariant)

	if cchainCode != 0 {
		fmt.Fprintf(os.Stderr, "\nC-Chain tests failed with exit code %d\n", cchainCode)
	} else {
		fmt.Println("\nC-Chain tests passed")
	}

	if subnetCode != 0 {
		fmt.Fprintf(os.Stderr, "Subnet-EVM tests failed with exit code %d\n", subnetCode)
	} else {
		fmt.Println("Subnet-EVM tests passed")
	}

	if cchainCode != 0 {
		return cchainCode
	}
	if subnetCode != 0 {
		return subnetCode
	}

	return 0
}

// runWith executes tests with the specified emulation function and variant.
func runWith(m *testing.M, emulateFn func(func() error) error, variant Variant) int {
	var code int
	currentVariant.Store(int32(variant))
	_ = emulateFn(func() error {
		code = m.Run()
		return nil
	})
	currentVariant.Store(int32(UnknownVariant))
	return code
}
