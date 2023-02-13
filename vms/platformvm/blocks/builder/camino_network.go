// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/components/message"
	txBuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

var errUnknownCrossChainMessage = errors.New("unknown cross-chain message")

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

func (n *caminoNetwork) CrossChainAppRequest(_ context.Context, chainID ids.ID, _ uint32, _ time.Time, request []byte) error {
	n.ctx.Log.Debug("called CrossChainAppRequest message handler",
		zap.Stringer("chainID", chainID),
		zap.Int("messageLen", len(request)),
	)

	msg := &message.CaminoRewardMessage{}
	if _, err := message.Codec.Unmarshal(request, msg); err != nil {
		return errUnknownCrossChainMessage // this would be fatal
	}

	tx, err := n.txBuilder.NewRewardsImportTx()
	if err != nil {
		n.ctx.Log.Error("caminoCrossChainAppRequest couldn't create rewardsImportTx")
		return nil // we don't want fatal here
	}

	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	return n.blkBuilder.AddUnverifiedTx(tx)
}
