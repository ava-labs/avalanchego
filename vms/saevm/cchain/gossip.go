// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
)

var _ gossip.Gossipable = (*gossipTx)(nil)

type gossipTx tx.Tx

func toGossipTx(t *tx.Tx) *gossipTx {
	return (*gossipTx)(t)
}

func (t *gossipTx) GossipID() ids.ID { return t.toTx().ID() }
func (t *gossipTx) toTx() *tx.Tx     { return (*tx.Tx)(t) }

var _ gossip.Marshaller[*gossipTx] = gossipMarshaller{}

type gossipMarshaller struct{}

func (gossipMarshaller) MarshalGossip(t *gossipTx) ([]byte, error) {
	return t.toTx().Bytes()
}

func (gossipMarshaller) UnmarshalGossip(b []byte) (*gossipTx, error) {
	t, err := tx.Parse(b)
	return toGossipTx(t), err
}

var _ gossip.Set[*gossipTx] = (*gossipTxPool)(nil)

type gossipTxPool struct {
	*txpool.Txpool
}

func newGossipTxPool(pool *txpool.Txpool) *gossipTxPool {
	return &gossipTxPool{Txpool: pool}
}

func (p *gossipTxPool) Add(t *gossipTx) error {
	return p.Txpool.Add(t.toTx())
}

func (p *gossipTxPool) Iterate(yield func(*gossipTx) bool) {
	for t := range p.Txpool.Iter() {
		if !yield(toGossipTx(t)) {
			return
		}
	}
}
