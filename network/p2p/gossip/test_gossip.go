// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import "github.com/ava-labs/avalanchego/utils/set"

var (
	_ Gossipable   = (*testTx)(nil)
	_ Set[*testTx] = (*testSet)(nil)
)

type testTx struct {
	hash Hash
}

func (t *testTx) GetHash() Hash {
	return t.hash
}

func (t *testTx) Marshal() ([]byte, error) {
	return t.hash[:], nil
}

func (t *testTx) Unmarshal(bytes []byte) error {
	t.hash = Hash{}
	copy(t.hash[:], bytes)
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

func (t testSet) Get(filter func(gossipable *testTx) bool) []*testTx {
	result := make([]*testTx, 0)
	for tx := range t.set {
		if !filter(tx) {
			continue
		}
		result = append(result, tx)
	}

	return result
}

func (t testSet) GetFilter() ([]byte, []byte, error) {
	bloom, err := t.bloom.Bloom.MarshalBinary()
	return bloom, t.bloom.Salt[:], err
}
