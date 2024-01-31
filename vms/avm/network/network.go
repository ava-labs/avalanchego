// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

const txGossipHandlerID = 0

var (
	_ common.AppHandler    = (*Network)(nil)
	_ validators.Connector = (*Network)(nil)
)

type Network struct {
	*p2p.Network

	txPushGossiper        gossip.Accumulator[*txs.Tx]
	txPullGossiper        gossip.Gossiper
	txPullGossipFrequency time.Duration

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
	config Config,
) (*Network, error) {
	p2pNetwork, err := p2p.NewNetwork(ctx.Log, appSender, registerer, "p2p")
	if err != nil {
		return nil, err
	}

	marshaller := &txParser{
		parser: parser,
	}
	validators := p2p.NewValidators(
		p2pNetwork.Peers,
		ctx.Log,
		ctx.SubnetID,
		ctx.ValidatorState,
		config.MaxValidatorSetStaleness,
	)
	txGossipClient := p2pNetwork.NewClient(
		txGossipHandlerID,
		p2p.WithValidatorSampling(validators),
	)
	txGossipMetrics, err := gossip.NewMetrics(registerer, "tx")
	if err != nil {
		return nil, err
	}

	txPushGossiper := gossip.NewPushGossiper[*txs.Tx](
		marshaller,
		txGossipClient,
		txGossipMetrics,
		config.TargetGossipSize,
	)

	gossipMempool, err := newGossipMempool(
		mempool,
		registerer,
		ctx.Log,
		txVerifier,
		parser,
		config.ExpectedBloomFilterElements,
		config.ExpectedBloomFilterFalsePositiveProbability,
		config.MaxBloomFilterFalsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	var txPullGossiper gossip.Gossiper
	txPullGossiper = gossip.NewPullGossiper[*txs.Tx](
		ctx.Log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		config.PullGossipPollSize,
	)

	// Gossip requests are only served if a node is a validator
	txPullGossiper = gossip.ValidatorGossiper{
		Gossiper:   txPullGossiper,
		NodeID:     ctx.NodeID,
		Validators: validators,
	}

	handler := gossip.NewHandler[*txs.Tx](
		ctx.Log,
		marshaller,
		txPushGossiper,
		gossipMempool,
		txGossipMetrics,
		config.TargetGossipSize,
	)

	validatorHandler := p2p.NewValidatorHandler(
		p2p.NewThrottlerHandler(
			handler,
			p2p.NewSlidingWindowThrottler(
				config.PullGossipThrottlingPeriod,
				config.PullGossipThrottlingLimit,
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
		Network:               p2pNetwork,
		txPushGossiper:        txPushGossiper,
		txPullGossiper:        txPullGossiper,
		txPullGossipFrequency: config.PullGossipFrequency,
		ctx:                   ctx,
		parser:                parser,
		mempool:               gossipMempool,
		appSender:             appSender,

		recentTxs: &cache.LRU[ids.ID, struct{}]{
			Size: config.LegacyPushGossipCacheSize,
		},
	}, nil
}

func (n *Network) Gossip(ctx context.Context) {
	gossip.Every(ctx, n.ctx.Log, n.txPullGossiper, n.txPullGossipFrequency)
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

	if err := n.mempool.Add(tx); err == nil {
		txID := tx.ID()
		n.txPushGossiper.Add(tx)
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

// IssueTx attempts to add a tx to the mempool, after verifying it. If the tx is
// added to the mempool, it will attempt to push gossip the tx to random peers
// in the network using both the legacy and p2p SDK.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.mempool.Add(tx); err != nil {
		return err
	}
	return n.gossipTx(ctx, tx)
}

// IssueVerifiedTx attempts to add a tx to the mempool, without first verifying
// it. If the tx is added to the mempool, it will attempt to push gossip the tx
// to random peers in the network using both the legacy and p2p SDK.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueVerifiedTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.mempool.AddVerified(tx); err != nil {
		return err
	}
	return n.gossipTx(ctx, tx)
}

// gossipTx pushes the tx to peers using both the legacy and p2p SDK.
func (n *Network) gossipTx(ctx context.Context, tx *txs.Tx) error {
	n.txPushGossiper.Add(tx)
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

// gossipTxMessage pushes the tx message to peers using the legacy format.
// If the tx was recently gossiped, this function does nothing.
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
