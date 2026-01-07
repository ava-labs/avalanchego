// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/prometheus/client_golang/prometheus"
)

// SystemConfig provides all the configurations needed to create a gossip
// system.
//
// All fields are optional.
type SystemConfig struct {
	Log       logging.Logger
	Registry  prometheus.Registerer
	Namespace string

	HandlerID uint64 // Defaults to [p2p.TxGossipHandlerID]

	TargetMessageSize int // Defaults to 20 KiB

	ThrottlingPeriod time.Duration // Defaults to one hour
	RequestPeriod    time.Duration // Defaults to one request per second

	PushGossipParams   BranchingFactor // Defaults to 100 validators and top 90% of stake
	PushRegossipParams BranchingFactor // Defaults to 10 validators

	DiscardedPushCacheSize int           // Defaults to 16,384
	RegossipPeriod         time.Duration // Defaults to 30 seconds
}

func (c *SystemConfig) SetDefaults() {
	if c.Log == nil {
		c.Log = logging.NoLog{}
	}
	if c.Registry == nil {
		c.Registry = prometheus.NewRegistry()
	}
	if c.TargetMessageSize <= 0 {
		c.TargetMessageSize = 20 * units.KiB
	}
	if c.ThrottlingPeriod <= 0 {
		c.ThrottlingPeriod = time.Hour
	}
	if c.RequestPeriod <= 0 {
		c.RequestPeriod = time.Second
	}
	if c.PushGossipParams == (BranchingFactor{}) {
		c.PushGossipParams = BranchingFactor{
			StakePercentage: .9,
			Validators:      100,
		}
	}
	if c.PushRegossipParams == (BranchingFactor{}) {
		c.PushRegossipParams = BranchingFactor{
			Validators: 10,
		}
	}
	if c.DiscardedPushCacheSize <= 0 {
		c.DiscardedPushCacheSize = 16_384
	}
	if c.RegossipPeriod <= 0 {
		c.RegossipPeriod = 30 * time.Second
	}
}

// SystemSet is the backend interface required to construct a gossip system.
type SystemSet[T Gossipable] interface {
	HandlerSet[T]
	PullGossiperSet[T]
	PushGossiperSet
}

// NewSystem is a helper to construct the senders and receivers for a gossip
// protocol.
func NewSystem[T Gossipable](
	nodeID ids.NodeID,
	network *p2p.Network,
	validatorPeers *p2p.Validators,
	set SystemSet[T],
	marshaller Marshaller[T],
	c SystemConfig,
) (
	p2p.Handler,
	*ValidatorGossiper,
	*PushGossiper[T],
	error,
) {
	c.SetDefaults()

	metrics, err := NewMetrics(c.Registry, c.Namespace)
	if err != nil {
		return nil, nil, nil, err
	}

	handler := NewHandler(
		c.Log,
		marshaller,
		set,
		metrics,
		c.TargetMessageSize,
	)

	requestsPerPeerPerPeriod := float64(c.ThrottlingPeriod / c.RequestPeriod)
	throttledHandler, err := p2p.NewDynamicThrottlerHandler(
		c.Log,
		handler,
		validatorPeers,
		c.ThrottlingPeriod,
		requestsPerPeerPerPeriod,
		c.Registry,
		c.Namespace,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	validatorOnlyHandler := p2p.NewValidatorHandler(
		throttledHandler,
		validatorPeers,
		c.Log,
	)

	// Pull requests are filtered by validators and are throttled to prevent
	// spamming. Push messages are not filtered.
	type (
		appRequester interface {
			AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError)
		}
		appGossiper interface {
			AppGossip(context.Context, ids.NodeID, []byte)
		}
	)
	gossipHandler := struct {
		appRequester
		appGossiper
	}{
		appRequester: validatorOnlyHandler,
		appGossiper:  handler,
	}

	client := network.NewClient(c.HandlerID, validatorPeers)
	const pollSize = 1
	pullGossiper := NewPullGossiper[T](
		c.Log,
		marshaller,
		set,
		client,
		metrics,
		pollSize,
	)
	pullGossiperWhenValidator := &ValidatorGossiper{
		Gossiper:   pullGossiper,
		NodeID:     nodeID,
		Validators: validatorPeers,
	}

	pushGossiper, err := NewPushGossiper(
		marshaller,
		set,
		validatorPeers,
		client,
		metrics,
		c.PushGossipParams,
		c.PushRegossipParams,
		c.DiscardedPushCacheSize,
		c.TargetMessageSize,
		c.RegossipPeriod,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return gossipHandler, pullGossiperWhenValidator, pushGossiper, nil
}
