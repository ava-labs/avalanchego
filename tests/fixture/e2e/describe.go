// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import "github.com/onsi/ginkgo/v2"

const (
	// For label usage in ginkgo invocation, see: https://onsi.github.io/ginkgo/#spec-labels

	// Label for filtering a test that is not primarily a C-Chain test
	// but nonetheless uses the C-Chain. Intended to support
	// execution of all C-Chain tests by the coreth repo in an e2e job.
	UsesCChainLabel = "uses-c"
)

// DescribeXChain annotates the tests for X-Chain.
func DescribeXChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label("x"))
	return ginkgo.Describe("[X-Chain] "+text, args...)
}

// DescribeXChainSerial annotates serial tests for X-Chain.
func DescribeXChainSerial(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Serial)
	return DescribeXChain(text, args...)
}

// DescribePChain annotates the tests for P-Chain.
func DescribePChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label("p"))
	return ginkgo.Describe("[P-Chain] "+text, args...)
}

// DescribeCChain annotates the tests for C-Chain.
func DescribeCChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label("c"))
	return ginkgo.Describe("[C-Chain] "+text, args...)
}

// DescribeSimplex annotates the tests for Simplex.
func DescribeSimplex(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label("s"))
	return ginkgo.Describe("[Simplex] "+text, args...)
}
