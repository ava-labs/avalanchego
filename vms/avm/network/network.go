// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/message"
)

// We allow [recentTxsCacheSize] to be fairly large because we only store hashes
// in the cache, not entire transactions.
const recentTxsCacheSize = 512

var (
	_ common.AppHandler    = (*Network)(nil)
	_ validators.Connector = (*Network)(nil)
)

type Network struct {
	*p2p.Network

	txPushGossiper gossip.Accumulator[*gossipTx]
	txPullGossiper gossip.Gossiper

	ctx       *snow.Context
	parser    txs.Parser
	mempool   *gossipMempool
	appSender common.AppSender

	// gossip related attributes
	recentTxsLock sync.Mutex
	recentTxs     *cache.LRU[ids.ID, struct{}]
}

func New(
	ctx *snow.Context,
	parser txs.Parser,
	txVerifier TxVerifier,
	mempool mempool.Mempool,
	appSender common.AppSender,
	registerer prometheus.Registerer,
	txGossipHandlerID uint64,
	maxValidatorSetStaleness time.Duration,
	txGossipMaxGossipSize int,
	txGossipPollSize int,
	txGossipThrottlingPeriod time.Duration,
	txGossipThrottlingLimit int,
	maxExpectedElements uint64,
	txGossipFalsePositiveProbability,
	txGossipMaxFalsePositiveProbability float64,
) (*Network, error) {
	p2pNetwork, err := p2p.NewNetwork(ctx.Log, appSender, registerer, "p2p")
	if err != nil {
		return nil, err
	}

	marshaller := &gossipTxParser{
		parser: parser,
	}
	validators := p2p.NewValidators(p2pNetwork.Peers, ctx.Log, ctx.SubnetID, ctx.ValidatorState, maxValidatorSetStaleness)
	txGossipClient := p2pNetwork.NewClient(
		txGossipHandlerID,
		p2p.WithValidatorSampling(validators),
	)
	txGossipMetrics, err := gossip.NewMetrics(registerer, "tx")
	if err != nil {
		return nil, err
	}

	txPushGossiper := gossip.NewPushGossiper[*gossipTx](
		marshaller,
		txGossipClient,
		txGossipMetrics,
		txGossipMaxGossipSize,
	)

	gossipMempool, err := newGossipMempool(
		mempool,
		ctx.Log,
		txVerifier,
		parser,
		maxExpectedElements,
		txGossipFalsePositiveProbability,
		txGossipMaxFalsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	var txPullGossiper gossip.Gossiper
	txPullGossiper = gossip.NewPullGossiper[*gossipTx](
		ctx.Log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		txGossipPollSize,
	)

	// Gossip requests are only served if a node is a validator
	txPullGossiper = gossip.ValidatorGossiper{
		Gossiper:   txPullGossiper,
		NodeID:     ctx.NodeID,
		Validators: validators,
	}

	handler := gossip.NewHandler[*gossipTx](
		ctx.Log,
		marshaller,
		txPushGossiper,
		gossipMempool,
		txGossipMetrics,
		txGossipMaxGossipSize,
	)

	validatorHandler := p2p.NewValidatorHandler(
		p2p.NewThrottlerHandler(
			handler,
			p2p.NewSlidingWindowThrottler(
				txGossipThrottlingPeriod,
				txGossipThrottlingLimit,
			),
			ctx.Log,
		),
		validators,
		ctx.Log,
	)

	// We allow pushing txs between all peers, but only serve gossip requests
	// from validators
	txGossipHandler := txGossipHandler{
		appGossipHandler:  handler,
		appRequestHandler: validatorHandler,
	}

	if err := p2pNetwork.AddHandler(txGossipHandlerID, txGossipHandler); err != nil {
		return nil, err
	}

	return &Network{
		Network:        p2pNetwork,
		txPushGossiper: txPushGossiper,
		txPullGossiper: txPullGossiper,
		ctx:            ctx,
		parser:         parser,
		mempool:        gossipMempool,
		appSender:      appSender,

		recentTxs: &cache.LRU[ids.ID, struct{}]{
			Size: recentTxsCacheSize,
		},
	}, nil
}

func (n *Network) Gossip(ctx context.Context, frequency time.Duration) {
	gossip.Every(ctx, n.ctx.Log, n.txPullGossiper, frequency)
}

func (n *Network) AppGossip(ctx context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)

	msgIntf, err := message.Parse(msgBytes)
	if err != nil {
		n.ctx.Log.Debug("forwarding AppGossip message to SDK network",
			zap.String("reason", "failed to parse message"),
		)

		return n.Network.AppGossip(ctx, nodeID, msgBytes)
	}

	msg, ok := msgIntf.(*message.Tx)
	if !ok {
		n.ctx.Log.Debug("dropping unexpected message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	tx, err := n.parser.ParseTx(msg.Tx)
	if err != nil {
		n.ctx.Log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}

	err = n.mempool.Add(&gossipTx{
		tx: tx,
	})
	if err == nil {
		txID := tx.ID()
		n.txPushGossiper.Add(&gossipTx{tx: tx})
		if err := n.txPushGossiper.Gossip(ctx); err != nil {
			n.ctx.Log.Error("failed to gossip tx",
				zap.Stringer("txID", tx.ID()),
				zap.Error(err),
			)
		}
		n.gossipTxMessage(ctx, txID, msgBytes)
	}
	return nil
}

func (n *Network) IssueTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.mempool.Add(&gossipTx{
		tx: tx,
	}); err != nil {
		return err
	}

	return n.gossipTx(ctx, tx)
}

func (n *Network) IssueVerifiedTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.mempool.AddVerifiedTx(&gossipTx{
		tx: tx,
	}); err != nil {
		return err
	}

	return n.gossipTx(ctx, tx)
}

func (n *Network) gossipTx(ctx context.Context, tx *txs.Tx) error {
	n.txPushGossiper.Add(&gossipTx{tx: tx})
	if err := n.txPushGossiper.Gossip(ctx); err != nil {
		n.ctx.Log.Error("failed to gossip tx",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)
	}

	txBytes := tx.Bytes()
	msg := &message.Tx{
		Tx: txBytes,
	}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return err
	}

	txID := tx.ID()
	n.gossipTxMessage(ctx, txID, msgBytes)
	return nil
}

func (n *Network) gossipTxMessage(ctx context.Context, txID ids.ID, msgBytes []byte) {
	n.recentTxsLock.Lock()
	_, has := n.recentTxs.Get(txID)
	n.recentTxs.Put(txID, struct{}{})
	n.recentTxsLock.Unlock()

	// Don't gossip a transaction if it has been recently gossiped.
	if has {
		return
	}

	n.ctx.Log.Debug("gossiping tx",
		zap.Stringer("txID", txID),
	)

	if err := n.appSender.SendAppGossip(ctx, msgBytes); err != nil {
		n.ctx.Log.Error("failed to gossip tx",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
	}
}
