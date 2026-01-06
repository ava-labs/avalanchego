// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ common.AppHandler    = (*Network)(nil)
	_ validators.Connector = (*Network)(nil)
)

type Network struct {
	*p2p.Network

	log     logging.Logger
	mempool *gossipMempool

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
	parser txs.Parser,
	txVerifier TxVerifier,
	mempool mempool.Mempool[*txs.Tx],
	appSender common.AppSender,
	registerer prometheus.Registerer,
	config Config,
) (*Network, error) {
	validators := p2p.NewValidators(
		log,
		subnetID,
		vdrs,
		config.MaxValidatorSetStaleness,
	)

	p2pNetwork, err := p2p.NewNetwork(
		log,
		appSender,
		registerer,
		"p2p",
		validators,
	)
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

	marshaller := &txParser{
		parser: parser,
	}

	systemConfig := gossip.SystemConfig{
		Log:               log,
		Registry:          registerer,
		Namespace:         "tx_gossip",
		TargetMessageSize: config.TargetGossipSize,
		ThrottlingPeriod:  config.PullGossipThrottlingPeriod,
		RequestPeriod:     config.PullGossipFrequency,
		PushGossipParams: gossip.BranchingFactor{
			StakePercentage: config.PushGossipPercentStake,
			Validators:      config.PushGossipNumValidators,
			Peers:           config.PushGossipNumPeers,
		},
		PushRegossipParams: gossip.BranchingFactor{
			Validators: config.PushRegossipNumValidators,
			Peers:      config.PushRegossipNumPeers,
		},
		DiscardedPushCacheSize: config.PushGossipDiscardedCacheSize,
		RegossipPeriod:         config.PushGossipMaxRegossipFrequency,
	}
	// Set the defaults so that we can use `config.PullGossipFrequency` after
	// the default has been set.
	systemConfig.SetDefaults()
	pullGossiper, pushGossiper, err := gossip.NewSystem(
		nodeID,
		p2pNetwork,
		validators,
		gossipMempool,
		marshaller,
		systemConfig,
	)
	if err != nil {
		return nil, err
	}

	return &Network{
		Network:               p2pNetwork,
		log:                   log,
		mempool:               gossipMempool,
		txPushGossiper:        pushGossiper,
		txPushGossipFrequency: config.PushGossipFrequency,
		txPullGossiper:        pullGossiper,
		txPullGossipFrequency: systemConfig.RequestPeriod,
	}, nil
}

func (n *Network) PushGossip(ctx context.Context) {
	gossip.Every(ctx, n.log, n.txPushGossiper, n.txPushGossipFrequency)
}

func (n *Network) PullGossip(ctx context.Context) {
	gossip.Every(ctx, n.log, n.txPullGossiper, n.txPullGossipFrequency)
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
