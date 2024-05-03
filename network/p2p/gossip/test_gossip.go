// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Gossipable          = (*testTx)(nil)
	_ Set[*testTx]        = (*testSet)(nil)
	_ Marshaller[*testTx] = (*testMarshaller)(nil)
)

type testTx struct {
	id ids.ID
}

func (t *testTx) GossipID() ids.ID {
	return t.id
}

type testMarshaller struct{}

func (testMarshaller) MarshalGossip(tx *testTx) ([]byte, error) {
	return tx.id[:], nil
}

func (testMarshaller) UnmarshalGossip(bytes []byte) (*testTx, error) {
	id, err := ids.ToID(bytes)
	return &testTx{
		id: id,
	}, err
}

type testSet struct {
	txs   map[ids.ID]*testTx
	bloom *BloomFilter
	onAdd func(tx *testTx)
}

func (t *testSet) Add(gossipables ...*testTx) []error {
	errs := make([]error, len(gossipables))
	for i, gossipable := range gossipables {
		if _, ok := t.txs[gossipable.id]; ok {
			errs[i] = fmt.Errorf("%s already present", gossipable.id)
			continue
		}

		t.txs[gossipable.id] = gossipable
		t.bloom.Add(gossipable)
		if t.onAdd != nil {
			t.onAdd(gossipable)
		}
	}

	return errs
}

func (t *testSet) Has(gossipID ids.ID) bool {
	_, ok := t.txs[gossipID]
	return ok
}

func (t *testSet) Iterate(f func(gossipable *testTx) bool) {
	for _, tx := range t.txs {
		if !f(tx) {
			return
		}
	}
}

func (t *testSet) GetFilter() ([]byte, []byte) {
	return t.bloom.Marshal()
}
