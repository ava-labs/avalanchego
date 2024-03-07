// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

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

	txPushGossiper        *gossip.PushGossiper[*txs.Tx]
	txPushGossipFrequency time.Duration
	txPullGossiper        gossip.Gossiper
	txPullGossipFrequency time.Duration

	ctx       *snow.Context
	parser    txs.Parser
	mempool   *gossipMempool
	appSender common.AppSender
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

	txPushGossiper, err := gossip.NewPushGossiper[*txs.Tx](
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		gossip.BranchingFactor{
			Validators: config.PushGossipNumValidators,
			Peers:      config.PushGossipNumPeers,
		},
		gossip.BranchingFactor{
			Validators: config.PushRegossipNumValidators,
			Peers:      config.PushRegossipNumPeers,
		},
		config.PushGossipDiscardedCacheSize,
		config.TargetGossipSize,
		config.PushGossipMaxRegossipFrequency,
	)
	if err != nil {
		return nil, err
	}

	var txPullGossiper gossip.Gossiper = gossip.NewPullGossiper[*txs.Tx](
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
		txPushGossipFrequency: config.PushGossipFrequency,
		txPullGossiper:        txPullGossiper,
		txPullGossipFrequency: config.PullGossipFrequency,
		ctx:                   ctx,
		parser:                parser,
		mempool:               gossipMempool,
		appSender:             appSender,
	}, nil
}

func (n *Network) PushGossip(ctx context.Context) {
	gossip.Every(ctx, n.ctx.Log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *Network) PullGossip(ctx context.Context) {
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

	if err := n.mempool.Add(tx); err != nil {
		n.ctx.Log.Debug("tx failed to be added to the mempool",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)
	}
	return nil
}

// IssueTxFromRPC attempts to add a tx to the mempool, after verifying it. If
// the tx is added to the mempool, it will attempt to push gossip the tx to
// random peers in the network.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueTxFromRPC(tx *txs.Tx) error {
	if err := n.mempool.Add(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}

// IssueTxFromRPCWithoutVerification attempts to add a tx to the mempool,
// without first verifying it. If the tx is added to the mempool, it will
// attempt to push gossip the tx to random peers in the network.
//
// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
// returned.
// If the tx is not added to the mempool, an error will be returned.
func (n *Network) IssueTxFromRPCWithoutVerification(tx *txs.Tx) error {
	if err := n.mempool.AddWithoutVerification(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}
