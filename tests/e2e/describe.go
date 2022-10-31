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

// DescribeXChain annotates the tests for X-Chain.
// Can run with any type of cluster (e.g., local, columbus, camino).
func DescribeXChain(text string, body func()) bool {
	return ginkgo.Describe("[X-Chain] "+text, body)
}

// DescribePChain annotates the tests for P-Chain.
// Can run with any type of cluster (e.g., local, fuji, mainnet).
func DescribePChain(text string, body func()) bool {
	return ginkgo.Describe("[P-Chain] "+text, body)
}
