// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package coretest

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"
)

// checkTxIndicesHelper checks that the transaction indices are correctly stored in the database.
// [expectedTail] is the expected value of the tail index.
// [indexedFrom] is the block number from which the transactions should be indexed.
// [indexedTo] is the block number to which the transactions should be indexed.
// [head] is the block number of the head block.
func CheckTxIndices(t *testing.T, expectedTail *uint64, indexedFrom uint64, indexedTo uint64, head uint64, db ethdb.Database, allowNilBlocks bool) {
	if expectedTail == nil {
		require.Nil(t, rawdb.ReadTxIndexTail(db))
	} else {
		var stored uint64
		tailValue := *expectedTail

		require.Eventually(t,
			func() bool {
				stored = *rawdb.ReadTxIndexTail(db)
				return tailValue == stored
			},
			30*time.Second, 500*time.Millisecond, "expected tail to be %d eventually", tailValue)
	}

	for i := uint64(0); i <= head; i++ {
		block := rawdb.ReadBlock(db, rawdb.ReadCanonicalHash(db, i), i)
		if block == nil && allowNilBlocks {
			continue
		}
		for _, tx := range block.Transactions() {
			index := rawdb.ReadTxLookupEntry(db, tx.Hash())
			if i < indexedFrom {
				require.Nilf(t, index, "Transaction indices should be deleted, number %d hash %s", i, tx.Hash().Hex())
			} else if i <= indexedTo {
				require.NotNilf(t, index, "Missing transaction indices, number %d hash %s", i, tx.Hash().Hex())
			}
		}
	}
}
