// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

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

var _ Client = (*client)(nil)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512
)

type Client interface {
	common.AppSender
	GossipTx(tx *txs.Tx) error
}

type client struct {
	common.AppSender

	log       logging.Logger
	recentTxs *cache.LRU[ids.ID, struct{}]
}

func NewClient(appSender common.AppSender, logger logging.Logger) Client {
	return &client{
		AppSender: appSender,
		recentTxs: &cache.LRU[ids.ID, struct{}]{Size: recentCacheSize},
		log:       logger,
	}
}

func (c *client) GossipTx(tx *txs.Tx) error {
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
