// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txMarshaller)(nil)
	_ gossip.Gossipable          = (*txs.Tx)(nil)
	_ gossip.Set[*txs.Tx]        = (*mempoolWithVerification)(nil)
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

type txMarshaller struct{}

func (txMarshaller) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (txMarshaller) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	return txs.Parse(txs.Codec, bytes)
}

func newMempoolWithVerification(
	mempool *mempool.Mempool,
	log logging.Logger,
	txVerifier TxVerifier,
) *mempoolWithVerification {
	return &mempoolWithVerification{
		Mempool:    mempool,
		log:        log,
		txVerifier: txVerifier,
	}
}

type mempoolWithVerification struct {
	*mempool.Mempool
	log        logging.Logger
	txVerifier TxVerifier
}

func (g *mempoolWithVerification) Add(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		return fmt.Errorf("tx %s dropped: %w", txID, txmempool.ErrDuplicateTx)
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	if err := g.txVerifier.VerifyTx(tx); err != nil {
		g.log.Debug("transaction failed verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		g.Mempool.MarkDropped(txID, err)
		return fmt.Errorf("failed verification: %w", err)
	}

	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}
	return nil
}

func (g *mempoolWithVerification) Has(txID ids.ID) bool {
	_, ok := g.Mempool.Get(txID)
	return ok
}
