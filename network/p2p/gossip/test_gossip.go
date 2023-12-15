// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Gossipable   = (*testTx)(nil)
	_ Set[*testTx] = (*testSet)(nil)
)

type testTx struct {
	id ids.ID
}

func (t *testTx) GetID() ids.ID {
	return t.id
}

func (t *testTx) Marshal() ([]byte, error) {
	return t.id[:], nil
}

func (t *testTx) Unmarshal(bytes []byte) error {
	t.id = ids.ID{}
	copy(t.id[:], bytes)
	return nil
}

type testSet struct {
	txs   map[ids.ID]*testTx
	bloom *BloomFilter
	onAdd func(tx *testTx)
}

func (t *testSet) Add(gossipable *testTx) error {
	if _, ok := t.txs[gossipable.id]; ok {
		return fmt.Errorf("%s already present", gossipable.id)
	}

	t.txs[gossipable.id] = gossipable
	t.bloom.Add(gossipable)
	if t.onAdd != nil {
		t.onAdd(gossipable)
	}

	return nil
}

func (t *testSet) Iterate(f func(gossipable *testTx) bool) {
	for _, tx := range t.txs {
		if !f(tx) {
			return
		}
	}
}

func (t *testSet) GetFilter() ([]byte, []byte, error) {
	bloom, err := t.bloom.Bloom.MarshalBinary()
	return bloom, t.bloom.Salt[:], err
}
