// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"encoding/binary"

	"github.com/ava-labs/avalanchego/network/p2p"
)

var _ gossipSender = (*gossipClient)(nil)

type gossipSender interface {
	sendGossip(ctx context.Context, bytes []byte) error
}

type gossipClient struct {
	client *p2p.Client
}

func (g *gossipClient) sendGossip(ctx context.Context, bytes []byte) error {
	return g.client.AppGossip(ctx, bytes)
}

type fakeGossipClient struct {
	handlerID uint64
	sent      chan<- []byte
}

func (f *fakeGossipClient) sendGossip(_ context.Context, bytes []byte) error {
	msg := append(binary.AppendUvarint(nil, f.handlerID), bytes...)
	f.sent <- msg
	return nil
}
