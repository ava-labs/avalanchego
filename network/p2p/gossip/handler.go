// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

var _ p2p.Handler = (*Handler[Gossipable])(nil)

// HandlerSet exposes the ability to add new values to the set in response to
// pushed information and for responding to pull requests.
//
// TODO: Consider naming this interface based on what it provides rather than
// how its used.
type HandlerSet[T Gossipable] interface {
	// Add adds a value to the set. Returns an error if v was not added.
	Add(v T) error
	// Iterate iterates over elements until f returns false.
	Iterate(f func(v T) bool)
}

func NewHandler[T Gossipable](
	log logging.Logger,
	marshaller Marshaller[T],
	set HandlerSet[T],
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
	set                HandlerSet[T]
	metrics            Metrics
	targetResponseSize int
}

func (h Handler[T]) AppRequest(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	filter, salt, err := ParseAppRequest(requestBytes)
	if err != nil {
		return nil, p2p.ErrUnexpected
	}

	var (
		hits         float64
		total        float64
		responseSize int
		gossipBytes  [][]byte
	)
	h.set.Iterate(func(gossipable T) bool {
		total++
		gossipID := gossipable.GossipID()

		// filter out what the requesting peer already knows about
		if bloom.Contains(filter, gossipID[:], salt[:]) {
			hits++
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

	if total > 0 {
		hitRate := float64(hits) / float64(total)
		h.metrics.bloomFilterHitRate.Observe(100 * hitRate)
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
