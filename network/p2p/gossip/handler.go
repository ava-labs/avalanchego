// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"errors"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
)

var (
	_ p2p.Handler = (*Handler[Gossipable])(nil)

	ErrInvalidID = errors.New("invalid id")
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

		// check that this doesn't exceed our maximum configured response size
		responseSize += len(bytes)
		if responseSize > h.maxResponseSize {
			return false
		}

		gossipBytes = append(gossipBytes, bytes)

		return true
	})

	if err != nil {
		return nil, err
	}

	response := &sdk.PullGossipResponse{
		Gossip: gossipBytes,
	}

	return proto.Marshal(response)
}
