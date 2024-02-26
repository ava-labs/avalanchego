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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

const TxGossipHandlerID = 0

type Network interface {
	common.AppHandler

	// PushGossip starts push gossiping transactions and blocks until it completes.
	PushGossip(ctx context.Context)
	// PullGossip starts pull gossiping transactions and blocks until it completes.
	PullGossip(ctx context.Context)
	// IssueTx verifies the transaction at the currently preferred state and adds
	// it to the mempool.
	IssueTx(context.Context, *txs.Tx) error
}

type network struct {
	*p2p.Network

	log                       logging.Logger
	txVerifier                TxVerifier
	mempool                   *gossipMempool
	partialSyncPrimaryNetwork bool
	appSender                 common.AppSender

	txPushGossiper        *gossip.PushGossiper[*txs.Tx]
	txPushGossipFrequency time.Duration
	txPullGossiper        gossip.Gossiper
	txPullGossipFrequency time.Duration
}

func New(
	log logging.Logger,
	nodeID ids.NodeID,
	subnetID ids.ID,
	vdrs validators.State,
	txVerifier TxVerifier,
	mempool mempool.Mempool,
	partialSyncPrimaryNetwork bool,
	appSender common.AppSender,
	registerer prometheus.Registerer,
	config Config,
) (Network, error) {
	p2pNetwork, err := p2p.NewNetwork(log, appSender, registerer, "p2p")
	if err != nil {
		return nil, err
	}

	marshaller := txMarshaller{}
	validators := p2p.NewValidators(
		p2pNetwork.Peers,
		log,
		subnetID,
		vdrs,
		config.MaxValidatorSetStaleness,
	)
	txGossipClient := p2pNetwork.NewClient(
		TxGossipHandlerID,
		p2p.WithValidatorSampling(validators),
	)
	txGossipMetrics, err := gossip.NewMetrics(registerer, "tx")
	if err != nil {
		return nil, err
	}

	gossipMempool, err := newGossipMempool(
		mempool,
		registerer,
		log,
		txVerifier,
		config.ExpectedBloomFilterElements,
		config.ExpectedBloomFilterFalsePositiveProbability,
		config.MaxBloomFilterFalsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	txPushGossiper := gossip.NewPushGossiper[*txs.Tx](
		log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		0,    // earlyGossipSize (IF THIS IS SMALLER THAN MAX SIZE, COULD LEAD TO GOSSIP EACH ADD)
		1024, // discardedSize
		config.TargetGossipSize,
		time.Second, // maxRegossipFrequency
	)

	var txPullGossiper gossip.Gossiper
	txPullGossiper = gossip.NewPullGossiper[*txs.Tx](
		log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		config.PullGossipPollSize,
	)

	// Gossip requests are only served if a node is a validator
	txPullGossiper = gossip.ValidatorGossiper{
		Gossiper:   txPullGossiper,
		NodeID:     nodeID,
		Validators: validators,
	}

	handler := gossip.NewHandler[*txs.Tx](
		log,
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
			log,
		),
		validators,
		log,
	)

	// We allow pushing txs between all peers, but only serve gossip requests
	// from validators
	txGossipHandler := txGossipHandler{
		appGossipHandler:  handler,
		appRequestHandler: validatorHandler,
	}

	if err := p2pNetwork.AddHandler(TxGossipHandlerID, txGossipHandler); err != nil {
		return nil, err
	}

	return &network{
		Network:                   p2pNetwork,
		log:                       log,
		txVerifier:                txVerifier,
		mempool:                   gossipMempool,
		partialSyncPrimaryNetwork: partialSyncPrimaryNetwork,
		appSender:                 appSender,
		txPushGossiper:            txPushGossiper,
		txPushGossipFrequency:     config.PushGossipFrequency,
		txPullGossiper:            txPullGossiper,
		txPullGossipFrequency:     config.PullGossipFrequency,
	}, nil
}

func (n *network) PushGossip(ctx context.Context) {
	// If the node is running partial sync, we should not perform any pull
	// gossip.
	if n.partialSyncPrimaryNetwork {
		return
	}

	gossip.Every(ctx, n.log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *network) PullGossip(ctx context.Context) {
	// If the node is running partial sync, we should not perform any pull
	// gossip.
	if n.partialSyncPrimaryNetwork {
		return
	}

	gossip.Every(ctx, n.log, n.txPullGossiper, n.txPullGossipFrequency)
}

func (n *network) AppGossip(ctx context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)

	if n.partialSyncPrimaryNetwork {
		n.log.Debug("dropping AppGossip message",
			zap.String("reason", "primary network is not being fully synced"),
		)
		return nil
	}

	msgIntf, err := message.Parse(msgBytes)
	if err != nil {
		n.log.Debug("forwarding AppGossip to p2p network",
			zap.String("reason", "failed to parse message"),
		)

		return n.Network.AppGossip(ctx, nodeID, msgBytes)
	}

	msg, ok := msgIntf.(*message.Tx)
	if !ok {
		n.log.Debug("dropping unexpected message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	tx, err := txs.Parse(txs.Codec, msg.Tx)
	if err != nil {
		n.log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}

	return n.issueTx(tx)
}

// TODO: is this only invoked by user submissions?
func (n *network) IssueTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.issueTx(tx); err != nil {
		return err
	}

	n.txPushGossiper.Add(tx)
	return nil
}

// returns nil if the tx is in the mempool
func (n *network) issueTx(tx *txs.Tx) error {
	// If we are partially syncing the Primary Network, we should not be
	// maintaining the transaction mempool locally.
	if n.partialSyncPrimaryNetwork {
		return nil
	}

	if err := n.mempool.Add(tx); err != nil {
		n.log.Debug("tx failed to be added to the mempool",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)

		return err
	}

	return nil
}
