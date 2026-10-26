// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import "github.com/onsi/ginkgo/v2"

const (
	// For label usage in ginkgo invocation, see: https://onsi.github.io/ginkgo/#spec-labels

	// UsesCChainLabel marks a test that is not primarily a C-Chain test but
	// nonetheless uses the C-Chain.
	UsesCChainLabel = "uses-c"

	// SkipSharedNetworkBootstrapCheckLabel excludes a test from bootstrap checks
	// against the shared network. A suite check still runs if another test is eligible.
	SkipSharedNetworkBootstrapCheckLabel = "skip-shared-network-bootstrap-check"
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
