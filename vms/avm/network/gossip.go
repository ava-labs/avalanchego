// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Set[*txs.Tx]        = (*mempoolWithVerification)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txParser)(nil)
)

// bloomChurnMultiplier is the number used to multiply the size of the mempool
// to determine how large of a bloom filter to create.
const bloomChurnMultiplier = 3

// txGossipHandler is the handler called when serving gossip messages
type txGossipHandler struct {
	p2p.NoOpHandler
	appGossipHandler  p2p.Handler
	appRequestHandler p2p.Handler
}

func (t txGossipHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	gossipBytes []byte,
) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t txGossipHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type txParser struct {
	parser txs.Parser
}

func (*txParser) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (g *txParser) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	return g.parser.ParseTx(bytes)
}

func newMempoolWithVerification(
	mempool mempool.Mempool[*txs.Tx],
	txVerifier TxVerifier,
) *mempoolWithVerification {
	return &mempoolWithVerification{
		Mempool:    mempool,
		txVerifier: txVerifier,
	}
}

type mempoolWithVerification struct {
	mempool.Mempool[*txs.Tx]
	txVerifier TxVerifier
}

// Add is called by the p2p SDK when handling transactions that were pushed to
// us and when handling transactions that were pulled from a peer. If this
// returns a nil error while handling push gossip, the p2p SDK will queue the
// transaction to push gossip as well.
func (g *mempoolWithVerification) Add(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		return fmt.Errorf("attempted to issue %w: %s ", mempool.ErrDuplicateTx, txID)
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	// Verify the tx at the currently preferred state
	if err := g.txVerifier.VerifyTx(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}

	return g.AddWithoutVerification(tx)
}

func (g *mempoolWithVerification) Has(txID ids.ID) bool {
	_, ok := g.Mempool.Get(txID)
	return ok
}

func (g *mempoolWithVerification) AddWithoutVerification(tx *txs.Tx) error {
	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(tx.ID(), err)
		return err
	}
	return nil
}
