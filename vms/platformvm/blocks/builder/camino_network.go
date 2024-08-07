// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
			recentTxs:  &cache.LRU[ids.ID, struct{}]{Size: recentCacheSize},
		},
		txBuilder: txBuilder,
	}
}

func (n *caminoNetwork) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, _ time.Time, request []byte) error {
	n.ctx.Log.Debug("called CrossChainAppRequest message handler",
		zap.Stringer("chainID", chainID),
		zap.Uint32("requestID", requestID),
		zap.Int("messageLen", len(request)),
	)

	msg := &message.CaminoRewardMessage{}
	if _, err := message.Codec.Unmarshal(request, msg); err != nil {
		return errUnknownCrossChainMessage // this would be fatal
	}

	if err := n.appSender.SendCrossChainAppResponse(
		ctx,
		chainID,
		requestID,
		[]byte(n.caminoRewardMessage()),
	); err != nil {
		n.ctx.Log.Error("caminoCrossChainAppRequest failed to send response", zap.Error(err))
		// we don't want fatal here: response is for logging only, so
		// its better to not respond properly, than crash the whole node
		return nil
	}

	return nil
}

func (n *caminoNetwork) caminoRewardMessage() string {
	tx, err := n.newRewardsImportTx()
	if err != nil {
		return err.Error()
	}

	utx, ok := tx.Unsigned.(*txs.RewardsImportTx)
	if !ok {
		// should never happen
		err = fmt.Errorf("unexpected tx type: expected *txs.RewardsImportTx, got %T", utx)
		n.ctx.Log.Error("caminoCrossChainAppRequest failed to create rewardsImportTx", zap.Error(err))
		return fmt.Sprintf("caminoCrossChainAppRequest failed to issue rewardsImportTx: %s", err)
	}

	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	if err := n.blkBuilder.AddUnverifiedTx(tx); err != nil {
		n.ctx.Log.Error("caminoCrossChainAppRequest failed to add unverified rewardsImportTx to block builder", zap.Error(err))
		return fmt.Sprintf("caminoCrossChainAppRequest failed to add unverified rewardsImportTx to block builder: %s", err)
	}

	amounts := make([]uint64, len(utx.Ins))
	for i := range utx.Ins {
		amounts[i] = utx.Ins[i].In.Amount()
	}

	return fmt.Sprintf("caminoCrossChainAppRequest issued rewardsImportTx with utxos with %v nCAM", amounts)
}

func (n *caminoNetwork) newRewardsImportTx() (*txs.Tx, error) {
	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	tx, err := n.txBuilder.NewRewardsImportTx()
	if err != nil {
		n.ctx.Log.Error("caminoCrossChainAppRequest failed to create rewardsImportTx", zap.Error(err))
		return nil, fmt.Errorf("caminoCrossChainAppRequest failed to create rewardsImportTx: %w", err)
	}
	return tx, nil
}
