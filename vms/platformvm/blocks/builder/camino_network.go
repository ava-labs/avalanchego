// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	txBuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

type caminoNetwork struct {
	network
	txBuilder txBuilder.CaminoBuilder
}

func NewCaminoNetwork(
	ctx *snow.Context,
	blkBuilder *caminoBuilder,
	appSender common.AppSender,
	txBuilder txBuilder.CaminoBuilder,
) Network {
	return &caminoNetwork{
		network: network{
			ctx:        ctx,
			blkBuilder: blkBuilder,
			appSender:  appSender,
			recentTxs:  &cache.LRU{Size: recentCacheSize},
		},
		txBuilder: txBuilder,
	}
}
