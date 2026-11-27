// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package ids builds NodeIDs for tests.
package ids

import (
	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/ids"
)

// ShortID returns the NodeID whose ShortID starts with [i].
func ShortID(i byte) common.NodeID {
	return common.NodeID{ShortID: ids.ShortID{i}}
}

// ShortIDs returns the NodeIDs ShortID(1) through ShortID(n).
func ShortIDs(n byte) []common.NodeID {
	nodes := make([]common.NodeID, n)
	for i := range n {
		nodes[i] = ShortID(i + 1)
	}
	return nodes
}
