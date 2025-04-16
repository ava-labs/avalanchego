// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

type Network struct {
	*p2p.Network

	log                       logging.Logger
	mempool                   *gossipMempool
	partialSyncPrimaryNetwork bool

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
	mempool mempool.Mempool[*txs.Tx],
	partialSyncPrimaryNetwork bool,
	appSender common.AppSender,
	stateLock sync.Locker,
	state state.Chain,
	signer warp.Signer,
	registerer prometheus.Registerer,
	config config.Network,
	notify func(),
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
		notify,
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

	// We allow all peers to request warp messaging signatures
	signatureRequestVerifier := signatureRequestVerifier{
		stateLock: stateLock,
		state:     state,
	}
	signatureRequestHandler := acp118.NewHandler(signatureRequestVerifier, signer)

	if err := p2pNetwork.AddHandler(acp118.HandlerID, signatureRequestHandler); err != nil {
		return nil, err
	}

	return &Network{
		Network:                   p2pNetwork,
		log:                       log,
		mempool:                   gossipMempool,
		partialSyncPrimaryNetwork: partialSyncPrimaryNetwork,
		txPushGossiper:            txPushGossiper,
		txPushGossipFrequency:     config.PushGossipFrequency,
		txPullGossiper:            txPullGossiper,
		txPullGossipFrequency:     config.PullGossipFrequency,
	}, nil
}

func (n *Network) PushGossip(ctx context.Context) {
	gossip.Every(ctx, n.log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *Network) PullGossip(ctx context.Context) {
	// If the node is running partial sync, we do not perform any pull gossip
	// because we should never be a validator.
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
	if err := n.mempool.Add(tx); err != nil {
		return err
	}
	n.txPushGossiper.Add(tx)
	return nil
}
