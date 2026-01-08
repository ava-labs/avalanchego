// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
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
	peers                 *p2p.Peers
}

func New(
	log logging.Logger,
	nodeID ids.NodeID,
	subnetID ids.ID,
	vdrs validators.State,
	txVerifier TxVerifier,
	mempool *mempool.Mempool,
	partialSyncPrimaryNetwork bool,
	appSender common.AppSender,
	stateLock sync.Locker,
	state state.Chain,
	signer warp.Signer,
	registerer prometheus.Registerer,
	config config.Network,
) (*Network, error) {
	validators := p2p.NewValidators(
		log,
		subnetID,
		vdrs,
		config.MaxValidatorSetStaleness,
	)
	peers := &p2p.Peers{}
	p2pNetwork, err := p2p.NewNetwork(
		log,
		appSender,
		registerer,
		"p2p",
		validators,
		peers,
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
	handler, pullGossiper, pushGossiper, err := gossip.NewSystem(
		nodeID,
		p2pNetwork,
		validators,
		gossipMempool,
		txMarshaller{},
		systemConfig,
	)
	if err != nil {
		return nil, err
	}

	if err := p2pNetwork.AddHandler(p2p.TxGossipHandlerID, handler); err != nil {
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
		txPushGossiper:            pushGossiper,
		txPushGossipFrequency:     config.PushGossipFrequency,
		txPullGossiper:            pullGossiper,
		txPullGossipFrequency:     systemConfig.RequestPeriod,
		peers:                     peers,
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

func (n *Network) Peers() *p2p.Peers {
	return n.peers
}
