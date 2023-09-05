// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

const (
	// Labels for test filtering
	// For usage in ginkgo invocation, see: https://onsi.github.io/ginkgo/#spec-labels
	XChainLabel     = "x"
	PChainLabel     = "p"
	CChainLabel     = "c"
	UsesCChainLabel = "uses-c"
)

// DescribeXChain annotates the tests for X-Chain.
func DescribeXChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label(XChainLabel))
	return ginkgo.Describe("[X-Chain] "+text, args...)
}

// DescribeXChainSerial annotates serial tests for X-Chain.
func DescribeXChainSerial(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Serial)
	return DescribeXChain(text, args...)
}

// DescribePChain annotates the tests for P-Chain.
func DescribePChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label(PChainLabel))
	return ginkgo.Describe("[P-Chain] "+text, args...)
}

// DescribeCChain annotates the tests for C-Chain.
func DescribeCChain(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label(CChainLabel))
	return ginkgo.Describe("[C-Chain] "+text, args...)
}
