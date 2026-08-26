// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestErrorSentinels(t *testing.T) {
	synctest.RequireDistinctAppErrors(t, map[string]*avacommon.AppError{
		"errWrongStartKeyLength":    errWrongStartKeyLength,
		"errZeroKeyLimit":           errZeroKeyLimit,
		"errWrongAccountHashLength": errWrongAccountHashLength,
		"errWrongRootLength":        errWrongRootLength,
		"errMissingRoot":            errMissingRoot,
		"errEmptyRoot":              errEmptyRoot,
		"errRootNotFound":           errRootNotFound,
	})
}

func newLeafResponder(tb testing.TB, trieDB *triedb.Database, opts ...HandlerOption) *responder {
	tb.Helper()
	return newResponder(loggingtest.New(tb, logging.Debug), trieDB, common.HashLength, opts...)
}

func TestResponder_AppErrors(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	tests := []struct {
		name    string
		req     *syncpb.GetLeafRequest
		wantErr *avacommon.AppError
	}{
		{
			name: "start_key_wrong_length",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: []byte{0x01, 0x02},
				KeyLimit: 10,
			},
			wantErr: errWrongStartKeyLength,
		},
		{
			name: "zero_key_limit",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 0,
			},
			wantErr: errZeroKeyLimit,
		},
		{
			name: "account_hash_wrong_length",
			req: &syncpb.GetLeafRequest{
				RootHash:    root.Bytes(),
				AccountHash: []byte{0x01, 0x02},
				KeyLimit:    10,
			},
			wantErr: errWrongAccountHashLength,
		},
		{
			name: "root_hash_wrong_length",
			req: &syncpb.GetLeafRequest{
				RootHash: []byte{0x01, 0x02},
				KeyLimit: 10,
			},
			wantErr: errWrongRootLength,
		},
		{
			name: "missing_root_hash",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{}.Bytes(),
				KeyLimit: 10,
			},
			wantErr: errMissingRoot,
		},
		{
			name: "empty_root_hash",
			req: &syncpb.GetLeafRequest{
				RootHash: types.EmptyRootHash.Bytes(),
				KeyLimit: 10,
			},
			wantErr: errEmptyRoot,
		},
		{
			name: "root_not_found",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{0x01}.Bytes(),
				KeyLimit: 10,
			},
			wantErr: errRootNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLeafResponder(t, trieDB)
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), tt.req)
			require.ErrorIs(t, appErr, tt.wantErr)
			require.Nil(t, resp)
		})
	}
}

func TestResponder(t *testing.T) {
	t.Parallel()

	const numLeaves = 50

	// Requests are root-addressed, so every trie shares one database.
	trieDB := synctest.NewTrieDB()
	accountRoot, accountKeys, accountVals, accountSnap := synctest.FillAccountTrie(t, trieDB, numLeaves)
	storageRoot, storageKeys, storageVals := synctest.FillTrie(t, trieDB, numLeaves)
	// More leaves than one response may carry.
	overfullRoot, overfullKeys, overfullVals := synctest.FillTrie(t, trieDB, maxLimit+200)

	tests := []struct {
		name     string
		snapshot *synctest.StaticSnapshot
		req      LeafRange
		want     Leaves
		wantMore bool
	}{
		{
			name: "whole_trie",
			req:  LeafRange{Root: accountRoot, Limit: numLeaves},
			want: Leaves{Keys: accountKeys, Vals: accountVals},
		},
		{
			name:     "whole_trie_from_a_snapshot",
			snapshot: accountSnap,
			req:      LeafRange{Root: accountRoot, Limit: numLeaves},
			want:     Leaves{Keys: accountKeys, Vals: accountVals},
		},
		{
			name:     "partial_range",
			req:      LeafRange{Root: accountRoot, Limit: 20},
			want:     Leaves{Keys: accountKeys[:20], Vals: accountVals[:20]},
			wantMore: true,
		},
		{
			name:     "partial_range_from_a_snapshot",
			snapshot: accountSnap,
			req:      LeafRange{Root: accountRoot, Limit: 20},
			want:     Leaves{Keys: accountKeys[:20], Vals: accountVals[:20]},
			wantMore: true,
		},
		{
			name: "range_to_the_end",
			req:  LeafRange{Root: accountRoot, Start: accountKeys[10], Limit: numLeaves},
			want: Leaves{Keys: accountKeys[10:], Vals: accountVals[10:]},
		},
		{
			name:     "range_to_the_end_from_a_snapshot",
			snapshot: accountSnap,
			req:      LeafRange{Root: accountRoot, Start: accountKeys[10], Limit: numLeaves},
			want:     Leaves{Keys: accountKeys[10:], Vals: accountVals[10:]},
		},
		{
			name: "storage_trie_without_account",
			req:  LeafRange{Root: storageRoot, Limit: numLeaves},
			want: Leaves{Keys: storageKeys, Vals: storageVals},
		},
		{
			name: "storage_trie_with_wrong_account",
			req:  LeafRange{Root: storageRoot, Account: utils.PointerTo(common.HexToHash("0xa11ce")), Limit: numLeaves},
			want: Leaves{Keys: storageKeys, Vals: storageVals},
		},
		{
			name:     "capped_at_max_limit",
			req:      LeafRange{Root: overfullRoot, Limit: maxLimit + 200},
			want:     Leaves{Keys: overfullKeys[:maxLimit], Vals: overfullVals[:maxLimit]},
			wantMore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			log := loggingtest.New(t, logging.Debug)
			net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
			require.NoError(t, RegisterHandler(
				log,
				net,
				p2p.EVMLeafRequestHandlerID,
				trieDB,
				common.HashLength,
				WithSnapshot(tt.snapshot),
			))
			client := NewClient(
				log,
				net,
				p2p.EVMLeafRequestHandlerID,
				common.HashLength,
				tracker,
			)

			got, more, err := client.FetchLeaves(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

// snapshotKinds is the leaf scopes every snapshot path must serve.
var snapshotKinds = []struct {
	name  string
	build func(*testing.T, *triedb.Database, int) snapshotCase
}{
	{
		name:  "account",
		build: newAccountCase,
	},
	{
		name:  "storage",
		build: newStorageCase,
	},
	{
		name:  "zero_account_storage",
		build: newZeroAccountCase,
	},
}

// snapshotCase is a trie plus a snapshot mirroring it, for one kind of leaf.
type snapshotCase struct {
	root        common.Hash
	accountHash []byte // nil for the account trie
	keys        [][]byte
	vals        [][]byte
	snap        *synctest.StaticSnapshot
	// leaves aliases the snapshot's pairs, so mutating it desyncs from the trie.
	leaves []synctest.StaticPair
}

// drop removes [from, to) from the snapshot only, so the trie holds leaves the
// snapshot lacks and a bridge overshoots the snapshot index.
func (c *snapshotCase) drop(from, to int) {
	kept := make([]synctest.StaticPair, 0, len(c.leaves))
	kept = append(kept, c.leaves[:from]...)
	kept = append(kept, c.leaves[to:]...)
	c.snap.Accounts = kept
	// leaves must keep aliasing the snapshot, otherwise a later corrupt
	// mutates a slice the snapshot no longer holds.
	c.leaves = kept
}

// corrupt points [from, to) at leaf 0's value, well formed but not matching the
// trie. A segment fails on any single mismatch.
func (c snapshotCase) corrupt(from, to int) {
	for i := from; i < to; i++ {
		c.leaves[i].V = c.leaves[0].V
	}
}

// trieSnapshot returns the case's flat view, an account view without an
// account hash, a storage view otherwise.
func (c snapshotCase) trieSnapshot() trieSnapshot {
	if len(c.accountHash) == 0 {
		return accountSnapshot{s: c.snap}
	}
	return storageSnapshot{s: c.snap, account: common.BytesToHash(c.accountHash)}
}

func newAccountCase(t *testing.T, trieDB *triedb.Database, n int) snapshotCase {
	root, keys, vals, snap := synctest.FillAccountTrie(t, trieDB, n)
	return snapshotCase{root: root, keys: keys, vals: vals, snap: snap, leaves: snap.Accounts}
}

func newStorageCase(t *testing.T, trieDB *triedb.Database, n int) snapshotCase {
	return newStorageCaseFor(t, trieDB, n, common.HexToHash("0xa11ce"))
}

// Genesis or a state upgrade can populate the zero account, so its storage
// trie must be served rather than aliased to the account trie.
func newZeroAccountCase(t *testing.T, trieDB *triedb.Database, n int) snapshotCase {
	return newStorageCaseFor(t, trieDB, n, common.Hash{})
}

func newStorageCaseFor(t *testing.T, trieDB *triedb.Database, n int, account common.Hash) snapshotCase {
	root, keys, vals := synctest.FillTrie(t, trieDB, n)

	// Storage slots are already trie-encoded, so the snapshot mirrors the trie.
	leaves := make([]synctest.StaticPair, len(keys))
	for i := range keys {
		leaves[i] = synctest.StaticPair{K: keys[i], V: vals[i]}
	}
	static := &synctest.StaticSnapshot{Storage: map[common.Hash][]synctest.StaticPair{account: leaves}}
	return snapshotCase{
		root:        root,
		accountHash: account.Bytes(),
		keys:        keys,
		vals:        vals,
		snap:        static,
		leaves:      leaves,
	}
}

// Whatever the snapshot holds, diverged leaves, a failed open, a failed
// iterator, the fill appends exactly the trie's leaves and reports more when
// the trie must serve the rest.
func TestFillFromSnapshot_NeverChangesLeaves(t *testing.T) {
	t.Parallel()

	// Two segments and a short third, so a tail case spans fewer leaves than a
	// whole segment.
	const numLeaves = 2*segmentLen + 2

	tests := []struct {
		name string
		// corruptFrom/corruptTo desync those leaves.
		corruptFrom int
		corruptTo   int
		openErr     error
		iterErr     error
		// wantLen is how many leaves the fill appends, the trie serves the rest.
		wantLen int
	}{
		{
			name:    "fast_path_serves_leaves",
			wantLen: numLeaves,
		},
		{
			name:        "slow_path_bridges_an_invalid_middle_segment",
			corruptFrom: segmentLen,
			corruptTo:   2 * segmentLen,
			wantLen:     numLeaves,
		},
		{
			name:      "invalid_head_segment",
			corruptTo: segmentLen,
			wantLen:   numLeaves,
		},
		// Nothing proves after the tail segment fails, so the fill ends at the
		// last proved segment.
		{
			name:        "invalid_tail_segment",
			corruptFrom: 2 * segmentLen,
			corruptTo:   numLeaves,
			wantLen:     2 * segmentLen,
		},
		{
			name:        "invalid_segment_boundary",
			corruptFrom: segmentLen - 1,
			corruptTo:   segmentLen + 1,
			wantLen:     numLeaves,
		},
		{
			name:      "all_invalid_appends_nothing",
			corruptTo: numLeaves,
		},
		{
			name:    "unopenable_snapshot_appends_nothing",
			openErr: errors.New("snapshot unavailable"),
		},
		{
			name:    "failing_iterator_appends_nothing",
			iterErr: errors.New("iteration failed"),
		},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)
				c.corrupt(tt.corruptFrom, tt.corruptTo)
				c.snap.OpenErr = tt.openErr
				c.snap.IterErr = tt.iterErr

				tr, err := trie.New(trie.TrieID(c.root), trieDB)
				require.NoError(t, err)

				r := newLeafRange(make([]byte, common.HashLength), numLeaves)
				more, err := fillFromSnapshot(c.trieSnapshot(), tr, r)
				require.NoError(t, err)
				require.Equal(t, c.keys[:tt.wantLen], r.keys)
				require.Equal(t, c.vals[:tt.wantLen], r.vals)
				require.Equal(t, tt.wantLen < numLeaves, more)

				// A wrongly scoped read consults another trie's flat data, which
				// only the read log can show.
				reads := c.snap.Reads()
				require.Len(t, reads, 1)
				require.Equal(t, common.BytesToHash(c.accountHash), reads[0].Account)
				require.Equal(t, len(c.accountHash) != 0, reads[0].Storage)
			})
		}
	}
}

// A fill never exceeds the range's capacity and appends only an exact prefix
// of the trie's leaves, whatever the snapshot holds. Every capacity here is
// below the trie's size, so more must always be reported.
func TestFillFromSnapshot_TrimsToCapacity(t *testing.T) {
	t.Parallel()

	const numAccounts = 300

	shapes := []struct {
		name        string
		corruptFrom int
		corruptTo   int
		dropFrom    int
		dropTo      int
	}{
		{
			name: "mirrors_the_trie",
		},
		{
			name:      "corrupt_head_segment",
			corruptTo: segmentLen,
		},
		{
			name:        "corrupt_middle_segment",
			corruptFrom: segmentLen,
			corruptTo:   2 * segmentLen,
		},
		{
			name:        "corrupt_segment_boundary",
			corruptFrom: segmentLen - 1,
			corruptTo:   segmentLen + 1,
		},
		{
			name:      "corrupt_everything",
			corruptTo: numAccounts,
		},
		// The bridge overshoots the missing leaves, so the capacity trims
		// proved segments, including one reaching the trie's end.
		{
			name:     "missing_leaves",
			dropFrom: 1,
			dropTo:   100,
		},
	}

	capacities := []int{1, segmentLen - 1, segmentLen, segmentLen + 1, 2*segmentLen + 1, numAccounts - 1}

	for _, capacity := range capacities {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("capacity=%d/%s", capacity, shape.name), func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := newAccountCase(t, trieDB, numAccounts)
				c.corrupt(shape.corruptFrom, shape.corruptTo)
				c.drop(shape.dropFrom, shape.dropTo)

				tr, err := trie.New(trie.TrieID(c.root), trieDB)
				require.NoError(t, err)

				r := newLeafRange(make([]byte, common.HashLength), capacity)
				more, err := fillFromSnapshot(c.trieSnapshot(), tr, r)
				require.NoError(t, err)
				require.True(t, more)

				n := len(r.keys)
				require.LessOrEqual(t, n, capacity)
				require.Equal(t, c.keys[:n], r.keys)
				require.Equal(t, c.vals[:n], r.vals)
			})
		}
	}
}

// A real [snapshot.Tree] driven through the case the disk-layer read exists
// for: a root the tree has retired.
func TestFillFromSnapshot_ServesHistoricalRootFromDiskLayer(t *testing.T) {
	t.Parallel()

	const numAccounts = 100

	trieDB, disk := synctest.NewTrieDBWithDisk()
	oldRoot, keys, vals, _ := synctest.FillAccountTrie(t, trieDB, numAccounts)
	newRoot := synctest.AdvanceAccountTrie(t, trieDB, oldRoot, 30)
	require.NotEqual(t, oldRoot, newRoot)

	tree := synctest.NewSnapshotTree(t, disk, trieDB, newRoot)
	synctest.RequireRootRetired(t, tree, oldRoot)

	tr, err := trie.New(trie.TrieID(oldRoot), trieDB)
	require.NoError(t, err)

	r := newLeafRange(make([]byte, common.HashLength), numAccounts)
	more, err := fillFromSnapshot(accountSnapshot{s: tree}, tr, r)
	require.NoError(t, err)
	require.Equal(t, keys, r.keys)
	require.Equal(t, vals, r.vals)
	require.False(t, more)
}
