// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/firewood/statehistory"
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
// time, to a pruned Firewood state database with state history enabled and to
// a hash-scheme archival reference, then reads every account and slot at
// every block height through the history overlay and through the reference
// and requires them to match. Roots must match at every commit.
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
		cfg := DefaultConfig(t.TempDir())
		cfg.StateHistoryEnabled = true
		cfg.RevisionsInMemory = 2
		cfg.DeferredCommitInterval = 1
		fw := newTestDatabaseWithConfig(t, cfg)
		store := fw.TrieDB().Backend().(*TrieDB).HistoryStore()
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
		blockHash := func(height uint64) common.Hash { return common.Hash{0xb, byte(height >> 8), byte(height)} }

		root := types.EmptyRootHash
		var roots []common.Hash
		fs, err := state.New(root, fw, nil)
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
				var parentHash common.Hash
				if block > 0 {
					parentHash = blockHash(block - 1)
				}
				payload := stateconf.WithTrieDBUpdatePayload(parentHash, blockHash(block))
				fr, err := fs.Commit(block, true, stateconf.WithTrieDBUpdateOpts(payload))
				require.NoError(t, err, "firewood Commit(%d)", block)
				if fr == root {
					// An unchanged root never reaches Update through StateDB;
					// core.BlockChain notifies Firewood explicitly (blockchain.go).
					require.NoError(t, fw.TrieDB().Update(fr, root, block, nil, nil, payload))
				}
				hr, err := hs.Commit(block, true)
				require.NoError(t, err, "hash Commit(%d)", block)
				require.Equal(t, hr, fr, "root of block %d", block)
				require.NoError(t, fw.TrieDB().Commit(fr, false))
				require.NoError(t, hash.TrieDB().Commit(hr, false))
				root = fr
				roots = append(roots, root)
				fs, err = state.New(root, fw, nil)
				require.NoError(t, err)
				hs, err = state.New(root, hash, nil)
				require.NoError(t, err)
			}
		}

		for h, r := range roots {
			got, err := state.New(r, statehistory.NewOverlay(store, fw, uint64(h), r), nil)
			require.NoError(t, err, "history state at block %d", h)
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
