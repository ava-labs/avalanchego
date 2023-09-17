// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
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
	set   set.Set[*testTx]
	bloom *BloomFilter
	onAdd func(tx *testTx)
}

func (t testSet) Add(gossipable *testTx) error {
	t.set.Add(gossipable)
	t.bloom.Add(gossipable)
	if t.onAdd != nil {
		t.onAdd(gossipable)
	}

	return nil
}

func (t testSet) Iterate(f func(gossipable *testTx) bool) {
	for tx := range t.set {
		if !f(tx) {
			return
		}
	}
}

func (t testSet) GetFilter() ([]byte, []byte, error) {
	bloom, err := t.bloom.Bloom.MarshalBinary()
	return bloom, t.bloom.Salt[:], err
}
