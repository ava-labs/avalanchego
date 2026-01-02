// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package libevmtest provides test utilities for packages that need to test
// both C-Chain and Subnet-EVM type registration.
package libevmtest

import (
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
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

// RunWithAll runs all tests with both C-Chain and Subnet-EVM type registration.
// Tests are run twice - once with C-Chain types and once with Subnet-EVM types.
// Both test runs must pass for the overall result to be successful.
//
// The currentVariant atomic is set before each test run, allowing tests to check
// which variant they're running under via currentVariant.Get().
func RunWithAll(m *testing.M, currentVariant *utils.Atomic[Variant]) int {
	fmt.Println("=== Running tests with both C-Chain and Subnet-EVM ===")

	fmt.Println("--- Running C-Chain variant ---")
	cchainCode := runWith(m, currentVariant, emulate.CChain, CChainVariant)

	fmt.Println("--- Running Subnet-EVM variant ---")
	subnetCode := runWith(m, currentVariant, emulate.SubnetEVM, SubnetEVMVariant)

	if cchainCode != 0 {
		fmt.Fprintln(os.Stderr, "C-Chain tests failed")
	} else {
		fmt.Println("C-Chain tests passed")
	}

	if subnetCode != 0 {
		fmt.Fprintln(os.Stderr, "Subnet-EVM tests failed")
	} else {
		fmt.Println("Subnet-EVM tests passed")
	}

	if cchainCode != 0 || subnetCode != 0 {
		return 1
	}

	return 0
}

// runWith executes tests with the specified emulation function and variant.
func runWith(m *testing.M, currentVariant *utils.Atomic[Variant], emulateFn func(func() error) error, variant Variant) int {
	var code int
	currentVariant.Set(variant)
	_ = emulateFn(func() error {
		code = m.Run()
		return nil
	})
	currentVariant.Set(UnknownVariant)
	return code
}
