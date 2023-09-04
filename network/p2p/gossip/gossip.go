// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/network/p2p/gossip/proto/pb"
	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
)

// GossipableAny exists to help create non-nil pointers to a concrete Gossipable
// ref: https://stackoverflow.com/questions/69573113/how-can-i-instantiate-a-non-nil-pointer-of-type-argument-with-generic-go
type GossipableAny[T any] interface {
	*T
	Gossipable
}

type Config struct {
	Frequency time.Duration
	PollSize  int
}

func NewGossiper[T any, U GossipableAny[T]](
	config Config,
	log logging.Logger,
	set Set[U],
	client *p2p.Client,
) *Gossiper[T, U] {
	return &Gossiper[T, U]{
		config: config,
		log:    log,
		set:    set,
		client: client,
	}
}

type Gossiper[T any, U GossipableAny[T]] struct {
	config Config
	log    logging.Logger
	set    Set[U]
	client *p2p.Client
}

func (g *Gossiper[_, _]) Gossip(ctx context.Context) {
	gossipTicker := time.NewTicker(g.config.Frequency)
	defer gossipTicker.Stop()

	for {
		select {
		case <-gossipTicker.C:
			if err := g.gossip(ctx); err != nil {
				g.log.Warn("failed to gossip", zap.Error(err))
			}
		case <-ctx.Done():
			g.log.Debug("shutting down gossip")
			return
		}
	}
}

func (g *Gossiper[_, _]) gossip(ctx context.Context) error {
	bloom, salt, err := g.set.GetFilter()
	if err != nil {
		return err
	}

	request := &pb.PullGossipRequest{
		Filter: bloom,
		Salt:   salt,
	}
	msgBytes, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	for i := 0; i < g.config.PollSize; i++ {
		if err := g.client.AppRequestAny(ctx, msgBytes, g.handleResponse); err != nil {
			return err
		}
	}

	return nil
}

func (g *Gossiper[T, U]) handleResponse(
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	if err != nil {
		g.log.Debug(
			"failed gossip request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
		return
	}

	response := &pb.PullGossipResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		g.log.Debug("failed to unmarshal gossip response", zap.Error(err))
		return
	}

	for _, bytes := range response.Gossip {
		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			g.log.Debug(
				"failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		hash := gossipable.GetHash()
		g.log.Debug(
			"received gossip",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("hash", hash[:]),
		)
		if err := g.set.Add(gossipable); err != nil {
			g.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Binary("id", hash[:]),
				zap.Error(err),
			)
			continue
		}
	}
}
