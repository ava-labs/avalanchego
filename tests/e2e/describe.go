// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

// DescribeLocal annotates the tests that requires local network-runner.
// Can only run with local cluster.
func DescribeLocal(text string, body func()) bool {
	return ginkgo.Describe("[Local] "+text, body)
}

// DescribeXChain annotates the tests for X-Chain.
// Can run with any type of cluster (e.g., local, columbus, camino).
func DescribeXChain(text string, body func()) bool {
	return ginkgo.Describe("[X-Chain] "+text, body)
}
