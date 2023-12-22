// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txMarshaller)(nil)
	_ gossip.Gossipable          = (*txs.Tx)(nil)
)

// txGossipHandler is the handler called when serving gossip messages
type txGossipHandler struct {
	p2p.NoOpHandler
	appGossipHandler  p2p.Handler
	appRequestHandler p2p.Handler
}

func (t txGossipHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	gossipBytes []byte,
) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t txGossipHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, error) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type txMarshaller struct{}

func (t txMarshaller) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (t txMarshaller) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	parsed, err := txs.Parse(txs.Codec, bytes)
	if err != nil {
		return nil, err
	}

	return parsed, nil
}
