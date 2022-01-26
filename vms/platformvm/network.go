// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512
)

type network struct {
	log logging.Logger
	// gossip related attributes
	gossipActivationTime time.Time
	appSender            common.AppSender
	mempool              *blockBuilder
	vm                   *VM
	recentTxs            *cache.LRU
}

func newNetwork(activationTime time.Time, appSender common.AppSender, vm *VM) *network {
	n := &network{
		log:                  vm.ctx.Log,
		gossipActivationTime: activationTime,
		appSender:            appSender,
		mempool:              &vm.blockBuilder,
		vm:                   vm,
		recentTxs:            &cache.LRU{Size: recentCacheSize},
	}

	return n
}

func (n *network) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, msgBytes []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppResponse(nodeID ids.ShortID, requestID uint32, msgBytes []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppGossip(nodeID ids.ShortID, msgBytes []byte) error {
	n.log.Debug(
		"AppGossip message handler called from %s with %d bytes",
		nodeID.PrefixedString(constants.NodeIDPrefix),
		len(msgBytes),
	)

	if time.Now().Before(n.gossipActivationTime) {
		n.log.Debug("AppGossip message called before activation time")
		return nil
	}

	msgIntf, err := message.Parse(msgBytes)
	if err != nil {
		n.log.Debug("dropping AppGossip message due to failing to parse message")
		return nil
	}

	msg, ok := msgIntf.(*message.Tx)
	if !ok {
		n.log.Debug(
			"dropping unexpected message from %s",
			nodeID.PrefixedString(constants.NodeIDPrefix),
		)
		return nil
	}

	tx := &Tx{}
	if _, err := Codec.Unmarshal(msg.Tx, tx); err != nil {
		n.log.Verbo("AppGossip provided invalid tx: %s", err)
		return nil
	}
	unsignedBytes, err := Codec.Marshal(CodecVersion, &tx.UnsignedTx)
	if err != nil {
		n.log.Warn("AppGossip failed to marshal unsigned tx: %s", err)
		return nil
	}
	tx.Initialize(unsignedBytes, msg.Tx)

	txID := tx.ID()

	// We need to grab the context lock here to avoid racy behavior with
	// transaction verification + mempool modifications.
	n.vm.ctx.Lock.Lock()
	defer n.vm.ctx.Lock.Unlock()

	if n.mempool.WasDropped(txID) {
		// If the tx is being dropped - just ignore it
		return nil
	}

	// add to mempool
	if err = n.mempool.AddUnverifiedTx(tx); err != nil {
		n.log.Debug(
			"AppResponse failed AddUnverifiedTx from %s with: %s",
			nodeID.PrefixedString(constants.NodeIDPrefix),
			err,
		)
	}
	return nil
}

func (n *network) GossipTx(tx *Tx) error {
	txID := tx.ID()
	// Don't gossip a transaction if it has been recently gossiped.
	if _, has := n.recentTxs.Get(txID); has {
		return nil
	}
	n.recentTxs.Put(txID, nil)

	n.log.Debug("gossiping tx %s", txID)

	msg := &message.Tx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return fmt.Errorf("GossipTx: failed to build Tx message with: %w", err)
	}
	return n.appSender.SendAppGossip(msgBytes)
}
