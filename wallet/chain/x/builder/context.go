// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/logging"
)

const Alias = "X"

type Context struct {
	NetworkID        uint32
	BlockchainID     ids.ID
	AVAXAssetID      ids.ID
	BaseTxFee        uint64
	CreateAssetTxFee uint64
}

func NewSnowContext(
	networkID uint32,
	blockchainID ids.ID,
	avaxAssetID ids.ID,
) (*snow.Context, error) {
	lookup := ids.NewAliaser()
	return &snow.Context{
		NetworkID:   networkID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     blockchainID,
		XChainID:    blockchainID,
		AVAXAssetID: avaxAssetID,
		Log:         logging.NoLog{},
		BCLookup:    lookup,
	}, lookup.Alias(blockchainID, Alias)
}
