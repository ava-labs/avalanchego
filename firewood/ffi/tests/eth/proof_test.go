// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package eth

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var _ ffi.Maybe[[]byte] = nothing{}

type nothing struct{}

func (nothing) HasValue() bool {
	return false
}

func (nothing) Value() []byte {
	return nil
}

func FuzzRangeProofCreation(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		db := newFirewoodDB(t)
		numAccounts := len(data)

		if numAccounts == 0 {
			return
		}

		keys := make([][]byte, 0, numAccounts)
		values := make([][]byte, 0, numAccounts)
		batch := make([]ffi.BatchOp, 0, numAccounts)
		expected := make(map[common.Hash]int)
		for i := range numAccounts {
			addr := common.BytesToAddress(crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, uint64(i))).Bytes())
			accHash := crypto.Keccak256Hash(addr[:])

			codeHash := types.EmptyCodeHash
			if data[i]&1 == 0 {
				codeHash = crypto.Keccak256Hash([]byte{data[i]})
				expected[codeHash]++
			}

			acc := &types.StateAccount{
				Nonce:    1,
				Balance:  uint256.NewInt(100),
				Root:     types.EmptyRootHash,
				CodeHash: codeHash[:],
			}
			accountRLP, err := rlp.EncodeToBytes(acc)
			require.NoError(t, err)
			keys = append(keys, accHash.Bytes())
			values = append(values, accountRLP)
			batch = append(batch, ffi.Put(keys[i], values[i]))
		}

		root, err := db.Update(batch)
		require.NoErrorf(t, err, "%T.Update()", db)

		proof, err := db.RangeProof(root, nothing{}, nothing{}, uint32(numAccounts))
		require.NoErrorf(t, err, "%T.RangeProof()", db)
		require.NotNil(t, proof)

		err = proof.Verify(root, nothing{}, nothing{}, uint32(numAccounts))
		require.NoErrorf(t, err, "%T.Verify()", proof)

		seen := make(map[common.Hash]struct{})
		for h, err := range proof.CodeHashes() {
			require.NoErrorf(t, err, "%T.CodeHashes()", proof)

			ethHash := common.Hash(h)
			require.NotEqual(t, types.EmptyCodeHash, ethHash)

			// must be one we expected
			if _, ok := expected[ethHash]; !ok {
				t.Fatalf("%T.CodeHashes() returned unexpected code hash %s", proof, ethHash.Hex())
			}

			seen[ethHash] = struct{}{}
		}

		// log more information about mismatches
		if len(seen) != len(expected) {
			for h := range expected {
				if _, ok := seen[h]; !ok {
					t.Log("missing expected code hash", h.Hex())
				}
			}
			for h := range seen {
				if _, ok := expected[h]; !ok {
					t.Log("extra unexpected code hash", h.Hex())
				}
			}
		}
		require.Len(t, expected, len(seen), "%T.CodeHashes() returned wrong number of unique code hashes", proof)
	})
}
