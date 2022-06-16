// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Network = &network{}

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512
)

type Network interface {
	common.AppHandler
	GossipTx(tx *txs.Tx) error
	SetActivationTime(time.Time)
}

type network struct {
	ctx        *snow.Context
	blkBuilder BlockBuilder

	// gossip related attributes
	gossipActivationTime time.Time
	appSender            common.AppSender
	recentTxs            *cache.LRU
}

func NewNetwork(
	ctx *snow.Context,
	blkBuilder *blockBuilder,
	activationTime time.Time,
	appSender common.AppSender,
) Network {
	return &network{
		ctx:        ctx,
		blkBuilder: blkBuilder,

		gossipActivationTime: activationTime,
		appSender:            appSender,
		recentTxs:            &cache.LRU{Size: recentCacheSize},
	}
}

func (n *network) AppRequestFailed(nodeID ids.NodeID, requestID uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppRequest(nodeID ids.NodeID, requestID uint32, deadline time.Time, msgBytes []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppResponse(nodeID ids.NodeID, requestID uint32, msgBytes []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppGossip(nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug(
		"AppGossip message handler called from %s with %d bytes",
		nodeID,
		len(msgBytes),
	)

	if time.Now().Before(n.gossipActivationTime) {
		n.ctx.Log.Debug("AppGossip message called before activation time")
		return nil
	}

	msgIntf, err := message.Parse(msgBytes)
	if err != nil {
		n.ctx.Log.Debug("dropping AppGossip message due to failing to parse message")
		return nil
	}

	msg, ok := msgIntf.(*message.Tx)
	if !ok {
		n.ctx.Log.Debug(
			"dropping unexpected message from %s",
			nodeID,
		)
		return nil
	}

	tx, err := txs.Parse(txs.Codec, msg.Tx)
	if err != nil {
		n.ctx.Log.Verbo("AppGossip provided invalid tx: %s", err)
		return nil
	}

	txID := tx.ID()

	// We need to grab the context lock here to avoid racy behavior with
	// transaction verification + mempool modifications.
	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	if _, dropped := n.blkBuilder.GetDropReason(txID); dropped {
		// If the tx is being dropped - just ignore it
		return nil
	}

	// add to mempool
	if err = n.blkBuilder.AddUnverifiedTx(tx); err != nil {
		n.ctx.Log.Debug(
			"AppResponse failed AddUnverifiedTx from %s with: %s",
			nodeID,
			err,
		)
	}
	return nil
}

func (n *network) GossipTx(tx *txs.Tx) error {
	txID := tx.ID()
	// Don't gossip a transaction if it has been recently gossiped.
	if _, has := n.recentTxs.Get(txID); has {
		return nil
	}
	n.recentTxs.Put(txID, nil)

	n.ctx.Log.Debug("gossiping tx %s", txID)

	msg := &message.Tx{Tx: tx.Bytes()}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return fmt.Errorf("GossipTx: failed to build Tx message with: %w", err)
	}
	return n.appSender.SendAppGossip(msgBytes)
}

func (n *network) SetActivationTime(t time.Time) { n.gossipActivationTime = t }
