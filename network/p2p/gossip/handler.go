// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"fmt"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"
	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ p2p.Handler = (*Handler[testTx, *testTx])(nil)

func NewHandler[T any, U GossipableAny[T]](
	log logging.Logger,
	client *p2p.Client,
	set Set[U],
	metrics Metrics,
	targetResponseSize int,
) *Handler[T, U] {
	return &Handler[T, U]{
		Handler:            p2p.NoOpHandler{},
		log:                log,
		client:             client,
		set:                set,
		metrics:            metrics,
		targetResponseSize: targetResponseSize,
		pullLabels: prometheus.Labels{
			typeLabel: pullType,
		},
		pushLabels: prometheus.Labels{
			typeLabel: pushType,
		},
	}
}

type Handler[T any, U GossipableAny[T]] struct {
	p2p.Handler
	log                logging.Logger
	set                Set[U]
	client             *p2p.Client
	metrics            Metrics
	targetResponseSize int

	pullLabels prometheus.Labels
	pushLabels prometheus.Labels
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

	sentCountMetric, err := h.metrics.sentCount.GetMetricWith(h.pullLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get sent count metric: %w", err)
	}

	sentBytesMetric, err := h.metrics.sentBytes.GetMetricWith(h.pullLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get sent bytes metric: %w", err)
	}

	sentCountMetric.Add(float64(len(response.Gossip)))
	sentBytesMetric.Add(float64(responseSize))

	return proto.Marshal(response)
}

func (h Handler[T, U]) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	msg := &sdk.PushGossip{}
	if err := proto.Unmarshal(gossipBytes, msg); err != nil {
		h.log.Debug("failed to unmarshal gossip", zap.Error(err))
		return
	}

	forward := make([][]byte, 0, len(msg.Gossip))
	receivedBytes := 0
	sentBytes := 0
	for _, bytes := range msg.Gossip {
		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			h.log.Debug("failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		receivedBytes += len(bytes)

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
		sentBytes += len(bytes)
	}

	forwardMsg := &sdk.PushGossip{
		Gossip: forward,
	}

	forwardMsgBytes, err := proto.Marshal(forwardMsg)
	if err != nil {
		h.log.Error(
			"failed to marshal forward gossip message",
			zap.Error(err),
		)
		return
	}

	if err := h.client.AppGossip(ctx, forwardMsgBytes); err != nil {
		h.log.Error(
			"failed to forward gossip",
			zap.Error(err),
		)
		return
	}

	receivedCountMetric, err := h.metrics.receivedCount.GetMetricWith(h.pushLabels)
	if err != nil {
		h.log.Error("failed to get received count metric", zap.Error(err))
		return
	}

	receivedBytesMetric, err := h.metrics.receivedBytes.GetMetricWith(h.pushLabels)
	if err != nil {
		h.log.Error("failed to get received bytes metric", zap.Error(err))
		return
	}

	sentCountMetric, err := h.metrics.sentCount.GetMetricWith(h.pushLabels)
	if err != nil {
		h.log.Error("failed to get sent count metric", zap.Error(err))
		return
	}

	sentBytesMetric, err := h.metrics.sentBytes.GetMetricWith(h.pushLabels)
	if err != nil {
		h.log.Error("failed to get sent bytes metric", zap.Error(err))
		return
	}

	receivedCountMetric.Add(float64(len(msg.Gossip)))
	receivedBytesMetric.Add(float64(receivedBytes))
	sentCountMetric.Add(float64(len(forwardMsg.Gossip)))
	sentBytesMetric.Add(float64(sentBytes))
}
