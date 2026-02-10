// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp236

import (
	"github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
)

func DescribeACP236(text string, args ...interface{}) bool {
	args = append(args, ginkgo.Label("acp-236"))
	return e2e.DescribePChain("[ACP-236]", args...)
}
