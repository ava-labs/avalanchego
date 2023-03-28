// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Sender = (*sender)(nil)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512
)

type Sender interface {
	common.AppSender

	GossipTx(tx *txs.Tx) error
}

type sender struct {
	common.AppSender
	log       logging.Logger
	recentTxs *cache.LRU[ids.ID, struct{}]
}

func NewSender(appSender common.AppSender, logger logging.Logger) Sender {
	return &sender{
		AppSender: appSender,
		recentTxs: &cache.LRU[ids.ID, struct{}]{Size: recentCacheSize},
		log:       logger,
	}
}

func (c *sender) GossipTx(tx *txs.Tx) error {
	txID := tx.ID()
	// Don't gossip a transaction if it has been recently gossiped.
	if _, has := c.recentTxs.Get(txID); has {
		return nil
	}
	c.recentTxs.Put(txID, struct{}{})

	c.log.Debug("gossiping tx",
		zap.Stringer("txID", txID),
	)

	msg := &message.TxGossip{Tx: tx.Bytes()}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return fmt.Errorf("GossipTx: failed to build Tx message: %w", err)
	}
	return c.SendAppGossip(context.TODO(), msgBytes)
}
