// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"errors"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/prometheus/client_golang/prometheus"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils"
)

var (
	_ p2p.Handler = (*Handler[Gossipable])(nil)

	ErrInvalidID = errors.New("invalid id")
)

type HandlerConfig struct {
	Namespace          string
	TargetResponseSize int
}

func NewHandler[T Gossipable](
	set Set[T],
	config HandlerConfig,
	metrics prometheus.Registerer,
) (*Handler[T], error) {
	h := &Handler[T]{
		Handler:            p2p.NoOpHandler{},
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
	}

	err := utils.Err(
		metrics.Register(h.sentN),
		metrics.Register(h.sentBytes),
	)
	return h, err
}

type Handler[T Gossipable] struct {
	p2p.Handler
	set                Set[T]
	targetResponseSize int

	sentN     prometheus.Counter
	sentBytes prometheus.Counter
}

func (h Handler[T]) AppRequest(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
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
	h.set.Iterate(func(gossipable T) bool {
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
