// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"errors"
	"time"

	"github.com/golang/protobuf/proto"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip/proto/pb"
)

var (
	_ p2p.Handler = (*Handler[Gossipable])(nil)

	ErrInvalidHash = errors.New("invalid hash")
)

func NewHandler[T Gossipable](set Set[T], maxResponseSize int) *Handler[T] {
	return &Handler[T]{
		Handler:         p2p.NoOpHandler{},
		set:             set,
		maxResponseSize: maxResponseSize,
	}
}

type Handler[T Gossipable] struct {
	p2p.Handler
	set             Set[T]
	maxResponseSize int
}

func (h Handler[T]) AppRequest(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := &pb.PullGossipRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, err
	}

	if len(request.Salt) != hashLength {
		return nil, ErrInvalidHash
	}
	filter := &BloomFilter{
		Bloom: &bloomfilter.Filter{},
		Salt:  Hash{},
	}
	if err := filter.Bloom.UnmarshalBinary(request.Filter); err != nil {
		return nil, err
	}

	copy(filter.Salt[:], request.Salt)

	// filter out what the requesting peer already knows about
	unknown := h.set.Get(func(gossipable T) bool {
		return !filter.Has(gossipable)
	})

	responseSize := 0
	gossipBytes := make([][]byte, 0, len(unknown))
	for _, gossipable := range unknown {
		bytes, err := gossipable.Marshal()
		if err != nil {
			return nil, err
		}

		responseSize += len(bytes)
		if responseSize > h.maxResponseSize {
			break
		}

		gossipBytes = append(gossipBytes, bytes)
	}

	response := &pb.PullGossipResponse{
		Gossip: gossipBytes,
	}

	return proto.Marshal(response)
}
