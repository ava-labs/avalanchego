// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chaincreator

import "github.com/ava-labs/avalanchego/ids"

type Interface interface {
	// CreateChain creates a blockchain on this node
	CreateChain(
		subnetID ids.ID,
		chainID ids.ID,
		vmID ids.ID,
		genesis []byte,
		fxIDs []ids.ID,
	)
}
