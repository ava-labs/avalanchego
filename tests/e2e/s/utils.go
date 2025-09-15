// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package s

import (
	"bytes"
	"slices"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func getLeaderForRound(nodes []*tmpnet.Node, round uint64) *tmpnet.Node {
	return nodes[round%uint64(len(nodes))]
}

func sortNodes(nodes []*tmpnet.Node) {
	slices.SortFunc(nodes, func(a, b *tmpnet.Node) int {
		return bytes.Compare(a.NodeID[:], b.NodeID[:])
	})
}
