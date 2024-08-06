// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var errMempoolDisabledWithPartialSync = errors.New("mempool is disabled partial syncing")

type Network struct {
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
) (*Network, error) {
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
		p2p.TxGossipHandlerID,
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

	txPushGossiper, err := gossip.NewPushGossiper[*txs.Tx](
		marshaller,
		gossipMempool,
		validators,
		txGossipClient,
		txGossipMetrics,
		gossip.BranchingFactor{
			StakePercentage: config.PushGossipPercentStake,
			Validators:      config.PushGossipNumValidators,
			Peers:           config.PushGossipNumPeers,
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

	if err := p2pNetwork.AddHandler(p2p.TxGossipHandlerID, txGossipHandler); err != nil {
		return nil, err
	}

	return &Network{
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

func (n *Network) PushGossip(ctx context.Context) {
	// TODO: Even though the node is running partial sync, we should support
	// issuing transactions from the RPC.
	if n.partialSyncPrimaryNetwork {
		return
	}

	gossip.Every(ctx, n.log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *Network) PullGossip(ctx context.Context) {
	// If the node is running partial sync, we should not perform any pull
	// gossip.
	if n.partialSyncPrimaryNetwork {
		return
	}

	gossip.Every(ctx, n.log, n.txPullGossiper, n.txPullGossipFrequency)
}

func (n *Network) AppGossip(ctx context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	if n.partialSyncPrimaryNetwork {
		n.log.Debug("dropping AppGossip message",
			zap.String("reason", "primary network is not being fully synced"),
		)
		return nil
	}

	return n.Network.AppGossip(ctx, nodeID, msgBytes)
}

func (n *Network) IssueTxFromRPC(tx *txs.Tx) error {
	// If we are partially syncing the Primary Network, we should not be
	// maintaining the transaction mempool locally.
	//
	// TODO: We should still push the transaction to some peers when partial
	// syncing.
	if n.partialSyncPrimaryNetwork {
		return errMempoolDisabledWithPartialSync
	}

	if err := n.mempool.Add(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}
