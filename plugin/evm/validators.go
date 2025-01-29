// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type validatorSet struct {
	set set.Set[ids.NodeID]
}

func (v *validatorSet) Has(ctx context.Context, nodeID ids.NodeID) bool {
	return v.set.Contains(nodeID)
}
