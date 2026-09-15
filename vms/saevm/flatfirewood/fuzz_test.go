// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flatfirewood

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	fuzzAccounts = 4
	fuzzSlots    = 4
)

// Ops of the fuzzed stream, each followed by its operand bytes.
const (
	fzSetBalance   byte = iota // account, amount
	fzSetStorage               // account, slot, value
	fzClearStorage             // account, slot
	fzSelfDestruct             // account
	fzCommit                   // end the block
	fzMax
)

// FuzzHistoryMatchesHashDB applies an arbitrary op stream, one block at a
// time, to a flatfirewood state database that keeps only two Firewood
// revisions and to a hash-scheme archival reference, then reads every account
// and slot at every block root through both and requires them to match. Roots
// must match at every commit, so a Firewood/history divergence in either
// direction fails.
func FuzzHistoryMatchesHashDB(f *testing.F) {
	f.Add([]byte{
		fzSetBalance, 0, 5, fzSetStorage, 0, 1, 9, fzCommit,
		fzSetStorage, 0, 2, 3, fzCommit,
		fzSelfDestruct, 0, fzSetBalance, 1, 1, fzCommit,
		fzSetBalance, 0, 7, fzSetStorage, 0, 1, 1, fzCommit,
		fzCommit,                                           // empty block: root unchanged
		fzSelfDestruct, 0, fzSetStorage, 0, 1, 2, fzCommit, // destroy and recreate in one block
		fzClearStorage, 0, 1, fzCommit,
	})
	f.Fuzz(func(t *testing.T, stream []byte) {
		flat := newDB(t, rawdb.NewMemoryDatabase(), t.TempDir())
		t.Cleanup(func() { require.NoError(t, flat.TrieDB().Close()) })
		hash := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), triedb.HashDefaults)

		addr := func(b byte) common.Address { return common.Address{b%fuzzAccounts + 1} }
		slot := func(b byte) common.Hash { return common.Hash{b%fuzzSlots + 1} }
		next := func() byte {
			if len(stream) == 0 {
				return 0
			}
			b := stream[0]
			stream = stream[1:]
			return b
		}

		root := types.EmptyRootHash
		var roots []common.Hash
		fs, err := state.New(root, flat, nil)
		require.NoError(t, err)
		hs, err := state.New(root, hash, nil)
		require.NoError(t, err)
		both := func(fn func(*state.StateDB)) { fn(fs); fn(hs) }

		for len(stream) > 0 {
			switch next() % fzMax {
			case fzSetBalance:
				a, v := addr(next()), uint256.NewInt(uint64(next()))
				both(func(s *state.StateDB) { s.SetBalance(a, v) })
			case fzSetStorage:
				a, k, v := addr(next()), slot(next()), common.Hash{next()}
				both(func(s *state.StateDB) { s.SetState(a, k, v) })
			case fzClearStorage:
				a, k := addr(next()), slot(next())
				both(func(s *state.StateDB) { s.SetState(a, k, common.Hash{}) })
			case fzSelfDestruct:
				a := addr(next())
				both(func(s *state.StateDB) { s.SelfDestruct(a) })
			case fzCommit:
				block := uint64(len(roots))
				fr, err := fs.Commit(block, true)
				require.NoError(t, err, "flat Commit(%d)", block)
				hr, err := hs.Commit(block, true)
				require.NoError(t, err, "hash Commit(%d)", block)
				require.Equal(t, hr, fr, "root of block %d", block)
				require.NoError(t, flat.TrieDB().Commit(fr, false))
				require.NoError(t, hash.TrieDB().Commit(hr, false))
				root = fr
				roots = append(roots, root)
				fs, err = state.New(root, flat, nil)
				require.NoError(t, err)
				hs, err = state.New(root, hash, nil)
				require.NoError(t, err)
			}
		}

		for h, r := range roots {
			got, err := state.New(r, flat, nil)
			require.NoError(t, err, "flat state at block %d", h)
			want, err := state.New(r, hash, nil)
			require.NoError(t, err, "hash state at block %d", h)
			for a := range byte(fuzzAccounts) {
				acc := addr(a)
				require.Equal(t, want.Exist(acc), got.Exist(acc), "Exist(%v) at block %d", acc, h)
				require.Equal(t, want.GetBalance(acc), got.GetBalance(acc), "balance of %v at block %d", acc, h)
				require.Equal(t, want.GetNonce(acc), got.GetNonce(acc), "nonce of %v at block %d", acc, h)
				for k := range byte(fuzzSlots) {
					require.Equal(t, want.GetState(acc, slot(k)), got.GetState(acc, slot(k)), "slot %v of %v at block %d", slot(k), acc, h)
				}
			}
		}
	})
}
