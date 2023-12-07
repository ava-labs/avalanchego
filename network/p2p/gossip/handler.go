// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"
	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ p2p.Handler = (*Handler[testTx, *testTx])(nil)

type HandlerConfig struct {
	Namespace          string
	TargetResponseSize int
}

func NewHandler[T any, U GossipableAny[T]](
	log logging.Logger,
	client *p2p.Client,
	set Set[U],
	config HandlerConfig,
	metrics prometheus.Registerer,
) (*Handler[T, U], error) {
	h := &Handler[T, U]{
		Handler: p2p.NoOpHandler{},
		log:     log,
		gossipSender: &gossipClient{
			client: client,
		},
		set:                set,
		targetResponseSize: config.TargetResponseSize,
		sentN: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "gossip_sent_n",
			Help:      "amount of gossip sent (n)",
		}),
		sentBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "gossip_sent_bytes",
			Help:      "amount of gossip sent (bytes)",
		}),
		pushGossipReceivedN: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "push_gossip_received_n",
			Help:      "amount of push gossip received (n)",
		}),
		pushGossipReceivedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: config.Namespace,
			Name:      "push_gossip_received_bytes",
			Help:      "amount of push gossip received (n)",
		}),
	}

	err := utils.Err(
		metrics.Register(h.sentN),
		metrics.Register(h.sentBytes),
		metrics.Register(h.pushGossipReceivedN),
		metrics.Register(h.pushGossipReceivedBytes),
	)
	return h, err
}

type Handler[T any, U GossipableAny[T]] struct {
	p2p.Handler
	log                logging.Logger
	set                Set[U]
	gossipSender       gossipSender
	targetResponseSize int

	sentN                   prometheus.Counter
	sentBytes               prometheus.Counter
	pushGossipReceivedN     prometheus.Counter
	pushGossipReceivedBytes prometheus.Counter
}

func (h Handler[T, U]) AppRequest(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := &sdk.PullGossipRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, err
	}

	salt, err := ids.ToID(request.Salt)
	if err != nil {
		return nil, err
	}

	filter := &BloomFilter{
		Bloom: &bloomfilter.Filter{},
		Salt:  salt,
	}
	if err := filter.Bloom.UnmarshalBinary(request.Filter); err != nil {
		return nil, err
	}

	responseSize := 0
	gossipBytes := make([][]byte, 0)
	h.set.Iterate(func(gossipable U) bool {
		// filter out what the requesting peer already knows about
		if filter.Has(gossipable) {
			return true
		}

		var bytes []byte
		bytes, err = gossipable.Marshal()
		if err != nil {
			return false
		}

		// check that this doesn't exceed our maximum configured target response
		// size
		gossipBytes = append(gossipBytes, bytes)
		responseSize += len(bytes)

		return responseSize <= h.targetResponseSize
	})

	if err != nil {
		return nil, err
	}

	response := &sdk.PullGossipResponse{
		Gossip: gossipBytes,
	}

	h.sentN.Add(float64(len(response.Gossip)))
	h.sentBytes.Add(float64(responseSize))

	return proto.Marshal(response)
}

func (h Handler[T, U]) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	msg := &sdk.PushGossip{}
	if err := proto.Unmarshal(gossipBytes, msg); err != nil {
		h.log.Debug("failed to unmarshal gossip", zap.Error(err))
		return
	}

	forward := make([][]byte, 0, len(msg.Gossip))
	for _, bytes := range msg.Gossip {
		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			h.log.Debug("failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
		}

		if _, ok := h.set.Get(gossipable.GetID()); ok {
			continue
		}

		if err := h.set.Add(gossipable); err != nil {
			h.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("id", gossipable.GetID()),
				zap.Error(err),
			)
			continue
		}

		// continue gossiping messages we have not seen to other peers
		forward = append(forward, bytes)
	}

	forwardMsg := &sdk.PushGossip{
		Gossip: forward,
	}

	forwardMsgBytes, err := proto.Marshal(forwardMsg)
	if err != nil {
		h.log.Debug(
			"failed to marshal forward gossip message",
			zap.Error(err),
		)
	}

	if err := h.gossipSender.sendGossip(ctx, forwardMsgBytes); err != nil {
		h.log.Debug(
			"failed to forward gossip",
			zap.Error(err),
		)
	}
}
