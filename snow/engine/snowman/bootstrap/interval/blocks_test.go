// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name                 string
		tree                 *Tree
		lastAcceptedHeight   uint64
		height               uint64
		blkBytes             []byte
		expectedToPersist    bool
		expectedToWantParent bool
	}{
		{
			name: "height already accepted",
			tree: func() *Tree {
				tree, err := NewTree(memdb.New())
				require.NoError(t, err)
				return tree
			}(),
			lastAcceptedHeight:   1,
			height:               1,
			blkBytes:             []byte{1},
			expectedToPersist:    false,
			expectedToWantParent: false,
		},
		{
			name: "height already added",
			tree: func() *Tree {
				db := memdb.New()
				tree, err := NewTree(db)
				require.NoError(t, err)

				require.NoError(t, tree.Add(db, 1))
				return tree
			}(),
			lastAcceptedHeight:   0,
			height:               1,
			blkBytes:             []byte{1},
			expectedToPersist:    false,
			expectedToWantParent: false,
		},
		{
			name: "next block is desired",
			tree: func() *Tree {
				tree, err := NewTree(memdb.New())
				require.NoError(t, err)
				return tree
			}(),
			lastAcceptedHeight:   0,
			height:               2,
			blkBytes:             []byte{2},
			expectedToPersist:    true,
			expectedToWantParent: true,
		},
		{
			name: "next block is accepted",
			tree: func() *Tree {
				tree, err := NewTree(memdb.New())
				require.NoError(t, err)
				return tree
			}(),
			lastAcceptedHeight:   0,
			height:               1,
			blkBytes:             []byte{1},
			expectedToPersist:    true,
			expectedToWantParent: false,
		},
		{
			name: "next block already added",
			tree: func() *Tree {
				db := memdb.New()
				tree, err := NewTree(db)
				require.NoError(t, err)

				require.NoError(t, tree.Add(db, 1))
				return tree
			}(),
			lastAcceptedHeight:   0,
			height:               2,
			blkBytes:             []byte{2},
			expectedToPersist:    true,
			expectedToWantParent: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			wantsParent, err := Add(
				db,
				test.tree,
				test.lastAcceptedHeight,
				test.height,
				test.blkBytes,
			)
			require.NoError(err)
			require.Equal(test.expectedToWantParent, wantsParent)

			blkBytes, err := GetBlock(db, test.height)
			if test.expectedToPersist {
				require.NoError(err)
				require.Equal(test.blkBytes, blkBytes)
				require.True(test.tree.Contains(test.height))
			} else {
				require.ErrorIs(err, database.ErrNotFound)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(err)
	lastAcceptedHeight := uint64(1)
	height := uint64(5)
	blkBytes := []byte{5}

	_, err = Add(
		db,
		tree,
		lastAcceptedHeight,
		height,
		blkBytes,
	)
	require.NoError(err)

	// Verify that the database has the block.
	storedBlkBytes, err := GetBlock(db, height)
	require.NoError(err)
	require.Equal(blkBytes, storedBlkBytes)
	require.Equal(uint64(1), tree.Len())

	require.NoError(Remove(
		db,
		tree,
		height,
	))
	require.Zero(tree.Len())

	// Verify that the database no longer contains the block.
	_, err = GetBlock(db, height)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(tree.Len())
}

func generateBlockchain(length uint64) [][]byte {
	blocks := make([][]byte, length)
	for i := range blocks {
		blocks[i] = binary.AppendUvarint(nil, uint64(i))
	}
	return blocks
}
