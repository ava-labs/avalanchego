// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

type Staker interface {
	// Staked is called when [nodeID] starts its staking period for its
	// corresponding subnet.
	Staked(ctx context.Context, nodeID ids.NodeID, txID ids.ID) error
	// Unstaked is called when [nodeID] finishes its staking period for its
	// corresponding subnet.
	Unstaked(ctx context.Context, nodeID ids.NodeID, txID ids.ID) error
}
