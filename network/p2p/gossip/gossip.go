// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ Gossiper = (*ValidatorGossiper)(nil)
	_ Gossiper = (*PullGossiper[testTx, *testTx])(nil)
)

// Gossiper gossips Gossipables to other nodes
type Gossiper interface {
	// Gossip runs a cycle of gossip. Returns an error if we failed to gossip.
	Gossip(ctx context.Context) error
}

// GossipableAny exists to help create non-nil pointers to a concrete Gossipable
// ref: https://stackoverflow.com/questions/69573113/how-can-i-instantiate-a-non-nil-pointer-of-type-argument-with-generic-go
type GossipableAny[T any] interface {
	*T
	Gossipable
}

// ValidatorGossiper only calls [Gossip] if the given node is a validator
type ValidatorGossiper struct {
	Gossiper

	NodeID     ids.NodeID
	Validators p2p.ValidatorSet
}

func (v ValidatorGossiper) Gossip(ctx context.Context) error {
	if !v.Validators.Has(ctx, v.NodeID) {
		return nil
	}

	return v.Gossiper.Gossip(ctx)
}

type Config struct {
	Namespace string
	PollSize  int
}

func NewPullGossiper[T any, U GossipableAny[T]](
	config Config,
	log logging.Logger,
	set Set[U],
	client *p2p.Client,
	metrics prometheus.Registerer,
) (*PullGossiper[T, U], error) {
	p := &PullGossiper[T, U]{
		config: config,
		log:    log,
		set:    set,
		client: client,
		receivedN: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "gossip_received_n",
			Help:      "amount of gossip received (n)",
		}),
		receivedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "gossip_received_bytes",
			Help:      "amount of gossip received (bytes)",
		}),
	}

	err := utils.Err(
		metrics.Register(p.receivedN),
		metrics.Register(p.receivedBytes),
	)
	return p, err
}

type PullGossiper[T any, U GossipableAny[T]] struct {
	config        Config
	log           logging.Logger
	set           Set[U]
	client        *p2p.Client
	receivedN     prometheus.Counter
	receivedBytes prometheus.Counter
}

func (p *PullGossiper[_, _]) Gossip(ctx context.Context) error {
	bloom, salt, err := p.set.GetFilter()
	if err != nil {
		return err
	}

	request := &sdk.PullGossipRequest{
		Filter: bloom,
		Salt:   salt,
	}
	msgBytes, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	for i := 0; i < p.config.PollSize; i++ {
		if err := p.client.AppRequestAny(ctx, msgBytes, p.handleResponse); err != nil {
			return err
		}
	}

	return nil
}

func (p *PullGossiper[T, U]) handleResponse(
	_ context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err *common.AppError,
) {
	if err != nil {
		p.log.Debug(
			"failed gossip request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
		return
	}

	response := &sdk.PullGossipResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		p.log.Debug("failed to unmarshal gossip response", zap.Error(err))
		return
	}

	receivedBytes := 0
	for _, bytes := range response.Gossip {
		receivedBytes += len(bytes)

		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			p.log.Debug(
				"failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		hash := gossipable.GetID()
		p.log.Debug(
			"received gossip",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("id", hash),
		)
		if err := p.set.Add(gossipable); err != nil {
			p.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("id", hash),
				zap.Error(err),
			)
			continue
		}
	}

	p.receivedN.Add(float64(len(response.Gossip)))
	p.receivedBytes.Add(float64(receivedBytes))
}

// Every calls [Gossip] every [frequency] amount of time.
func Every(ctx context.Context, log logging.Logger, gossiper Gossiper, frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := gossiper.Gossip(ctx); err != nil {
				log.Warn("failed to gossip", zap.Error(err))
			}
		case <-ctx.Done():
			log.Debug("shutting down gossip")
			return
		}
	}
}
