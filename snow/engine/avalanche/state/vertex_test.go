// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

func TestVertexVerify(t *testing.T) {
	conflictingInputID := ids.NewID([32]byte{'i', 'n'})
	inputs := ids.Set{}
	inputs.Add(conflictingInputID)
	tx0 := &snowstorm.TestTx{
		Identifier: ids.NewID([32]byte{'t', 'x', '0'}),
		Deps:       nil,
		Ins:        inputs,
	}
	validVertex := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{2})},
		txs:       []snowstorm.Tx{tx0},
	}

	if err := validVertex.Verify(); err != nil {
		t.Fatalf("Valid vertex failed verification due to: %w", err)
	}

	nonUniqueParentsVtx := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{'d', 'u', 'p'}), ids.NewID([32]byte{'d', 'u', 'p'})},
		txs:       []snowstorm.Tx{tx0},
	}

	if err := nonUniqueParentsVtx.Verify(); err == nil {
		t.Fatal("Vertex with non unique parents should not have passed verification")
	}

	parent0 := ids.NewID([32]byte{0})
	parent1 := ids.NewID([32]byte{1})
	sortedParents := []ids.ID{parent0, parent1}
	ids.SortIDs(sortedParents)
	nonSortedParentsVtx := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{sortedParents[1], sortedParents[0]},
		txs:       []snowstorm.Tx{tx0},
	}

	if err := nonSortedParentsVtx.Verify(); err == nil {
		t.Fatal("Vertex with non-sorted parents should not have passed verification")
	}

	noTxsVertex := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{2})},
		txs:       []snowstorm.Tx{},
	}

	if err := noTxsVertex.Verify(); err == nil {
		t.Fatal("Vertex with no txs should not have passed verification")
	}

	tx1 := &snowstorm.TestTx{
		Identifier: ids.NewID([32]byte{'t', 'x', '1'}),
		Deps:       nil,
		Ins:        nil,
	}
	sortedTxs := []snowstorm.Tx{tx0, tx1}
	sortTxs(sortedTxs)
	unsortedTxsVertex := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{2})},
		txs:       []snowstorm.Tx{sortedTxs[1], sortedTxs[0]},
	}

	if err := unsortedTxsVertex.Verify(); err == nil {
		t.Fatal("Vertex with unsorted transactions should not have passed verification")
	}

	nonUniqueTxsVertex := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{2})},
		txs:       []snowstorm.Tx{tx0, tx0},
	}

	if err := nonUniqueTxsVertex.Verify(); err == nil {
		t.Fatal("Vertex with non-unique transactions should not have passed verification")
	}

	inputs.Add(ids.NewID([32]byte{'e', 'x', 't', 'r', 'a'}))
	conflictingTx := &snowstorm.TestTx{
		Identifier: ids.NewID([32]byte{'c', 'o', 'n', 'f', 'l', 'i', 'c', 't'}),
		Deps:       nil,
		Ins:        inputs,
	}

	conflictingTxs := []snowstorm.Tx{tx0, conflictingTx}
	sortTxs(conflictingTxs)

	conflictingTxsVertex := &vertex{
		id:        ids.NewID([32]byte{}),
		chainID:   ids.NewID([32]byte{1}),
		height:    1,
		parentIDs: []ids.ID{ids.NewID([32]byte{2})},
		txs:       conflictingTxs,
	}

	if err := conflictingTxsVertex.Verify(); err == nil {
		t.Fatal("Vertex with conflicting transactions should not have passed verification")
	}
}
