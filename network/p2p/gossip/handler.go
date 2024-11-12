// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ p2p.Handler = (*Handler[*testTx])(nil)

func NewHandler[T Gossipable](
	log logging.Logger,
	marshaller Marshaller[T],
	set Set[T],
	metrics Metrics,
	targetResponseSize int,
) *Handler[T] {
	return &Handler[T]{
		Handler:            p2p.NoOpHandler{},
		log:                log,
		marshaller:         marshaller,
		set:                set,
		metrics:            metrics,
		targetResponseSize: targetResponseSize,
	}
}

type Handler[T Gossipable] struct {
	p2p.Handler
	marshaller         Marshaller[T]
	log                logging.Logger
	set                Set[T]
	metrics            Metrics
	targetResponseSize int
}

func (h Handler[T]) AppRequest(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	filter, salt, err := ParseAppRequest(requestBytes)
	if err != nil {
		return nil, p2p.ErrUnexpected
	}

	responseSize := 0
	gossipBytes := make([][]byte, 0)
	h.set.Iterate(func(gossipable T) bool {
		gossipID := gossipable.GossipID()

		// filter out what the requesting peer already knows about
		if bloom.Contains(filter, gossipID[:], salt[:]) {
			return true
		}

		var bytes []byte
		bytes, err = h.marshaller.MarshalGossip(gossipable)
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
		return nil, p2p.ErrUnexpected
	}

	if err := h.metrics.observeMessage(sentPullLabels, len(gossipBytes), responseSize); err != nil {
		return nil, p2p.ErrUnexpected
	}

	response, err := MarshalAppResponse(gossipBytes)
	if err != nil {
		return nil, p2p.ErrUnexpected
	}

	return response, nil
}

func (h Handler[_]) AppGossip(_ context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	gossip, err := ParseAppGossip(gossipBytes)
	if err != nil {
		h.log.Debug("failed to unmarshal gossip", zap.Error(err))
		return
	}

	receivedBytes := 0
	for _, bytes := range gossip {
		receivedBytes += len(bytes)
		gossipable, err := h.marshaller.UnmarshalGossip(bytes)
		if err != nil {
			h.log.Debug("failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		if err := h.set.Add(gossipable); err != nil {
			h.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("id", gossipable.GossipID()),
				zap.Error(err),
			)
		}
	}

	if err := h.metrics.observeMessage(receivedPushLabels, len(gossip), receivedBytes); err != nil {
		h.log.Error("failed to update metrics",
			zap.Error(err),
		)
	}
}
