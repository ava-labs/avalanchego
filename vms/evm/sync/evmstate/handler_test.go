// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
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

// A typed-nil snapshot passed to [WithSnapshot] must be treated as if no
// snapshot was provided, rather than wrapped into a non-nil interface that
// could later panic.
func TestWithSnapshot_IgnoresNilPointer(t *testing.T) {
	t.Parallel()

	var absent *synctest.StaticSnapshot
	r := newLeafResponder(t, synctest.NewTrieDB(), WithSnapshot(absent))
	require.Nil(t, r.snapshot)
}

// serve registers the leaf handler for trieDB on a loopback network and returns
// a client bound to it, so a test drives both halves of the protocol.
func serve(t *testing.T, ctx context.Context, trieDB *triedb.Database, opts ...HandlerOption) *Client {
	t.Helper()
	log := loggingtest.New(t, logging.Debug)
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(log, net, p2p.EVMLeafRequestHandlerID, trieDB, common.HashLength, opts...))
	return NewClient(log, net, p2p.EVMLeafRequestHandlerID, common.HashLength, tracker)
}

func TestGetLeaves(t *testing.T) {
	t.Parallel()

	const numLeaves = 50

	// Requests are root-addressed, so every trie shares one database.
	trieDB := synctest.NewTrieDB()
	accountRoot, accountKeys, accountVals, _ := synctest.FillAccountTrie(t, trieDB, numLeaves)
	storageRoot, storageKeys, storageVals := synctest.FillTrie(t, trieDB, numLeaves)
	// More leaves than one response may carry.
	overfullRoot, overfullKeys, overfullVals := synctest.FillTrie(t, trieDB, maxLimit+200)

	tests := []struct {
		name     string
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
			name:     "partial_range",
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
			client := serve(t, ctx, trieDB)

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

// account returns the scope for a client request, nil for the account trie.
func (c snapshotCase) account() *common.Hash {
	if len(c.accountHash) == 0 {
		return nil
	}
	return utils.PointerTo(common.BytesToHash(c.accountHash))
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

// The snapshot is a latency optimisation. Whatever it holds, diverged leaves,
// a failed open, a failed iterator, the peer sees exactly the trie.
func TestRegisterHandler_SnapshotNeverChangesLeaves(t *testing.T) {
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
	}{
		{
			name: "fast_path_serves_leaves",
		},
		{
			name:        "slow_path_bridges_an_invalid_middle_segment",
			corruptFrom: segmentLen,
			corruptTo:   2 * segmentLen,
		},
		{
			name:      "invalid_head_segment",
			corruptTo: segmentLen,
		},
		{
			name:        "invalid_tail_segment",
			corruptFrom: 2 * segmentLen,
			corruptTo:   numLeaves,
		},
		{
			name:        "invalid_segment_boundary",
			corruptFrom: segmentLen - 1,
			corruptTo:   segmentLen + 1,
		},
		{
			name:      "all_invalid_falls_back_to_trie",
			corruptTo: numLeaves,
		},
		{
			name:    "unopenable_snapshot_falls_back_to_trie",
			openErr: errors.New("snapshot unavailable"),
		},
		{
			name:    "failing_iterator_falls_back_to_trie",
			iterErr: errors.New("iteration failed"),
		},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)
				c.corrupt(tt.corruptFrom, tt.corruptTo)
				c.snap.OpenErr = tt.openErr
				c.snap.IterErr = tt.iterErr

				client := serve(t, ctx, trieDB, WithSnapshot(c.snap))
				got, more, err := client.FetchLeaves(ctx, LeafRange{
					Root:    c.root,
					Account: c.account(),
					Limit:   numLeaves,
				})
				require.NoError(t, err)
				require.Equal(t, c.keys, got.Keys)
				require.Equal(t, c.vals, got.Vals)
				require.False(t, more)

				// The trie fallback serves the same leaves, so only the read log
				// proves the snapshot ran, and only its scope shows the right
				// trie was consulted.
				reads := c.snap.Reads()
				require.Len(t, reads, 1)
				require.Equal(t, common.BytesToHash(c.accountHash), reads[0].Account)
				require.Equal(t, len(c.accountHash) != 0, reads[0].Storage)
			})
		}
	}
}

// Every response is trimmed to the KeyLimit whatever the snapshot holds, and a
// trimmed response keeps its proof, otherwise the fetch never verifies.
func TestRegisterHandler_SnapshotTrimsToKeyLimit(t *testing.T) {
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
		// The bridge overshoots the missing leaves, so the limit trims proved
		// segments, including one reaching the trie's end.
		{
			name:     "missing_leaves",
			dropFrom: 1,
			dropTo:   100,
		},
	}

	limits := []uint16{1, segmentLen - 1, segmentLen, segmentLen + 1, 2*segmentLen + 1, numAccounts - 1}

	for _, limit := range limits {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("limit=%d/%s", limit, shape.name), func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				trieDB := synctest.NewTrieDB()
				c := newAccountCase(t, trieDB, numAccounts)
				c.corrupt(shape.corruptFrom, shape.corruptTo)
				c.drop(shape.dropFrom, shape.dropTo)

				client := serve(t, ctx, trieDB, WithSnapshot(c.snap))
				got, more, err := client.FetchLeaves(ctx, LeafRange{Root: c.root, Limit: limit})
				require.NoError(t, err)

				n := int(limit)
				require.Equal(t, c.keys[:n], got.Keys)
				require.Equal(t, c.vals[:n], got.Vals)
				require.True(t, more)
			})
		}
	}
}

// A real [snapshot.Tree] driven through the case the disk-layer read exists
// for: a root the tree has retired.
func TestRegisterHandler_ServesHistoricalRootFromDiskLayer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const numAccounts = 100

	trieDB, disk := synctest.NewTrieDBWithDisk()
	oldRoot, keys, vals, _ := synctest.FillAccountTrie(t, trieDB, numAccounts)
	newRoot := synctest.AdvanceAccountTrie(t, trieDB, oldRoot, 30)
	require.NotEqual(t, oldRoot, newRoot)

	tree := synctest.NewSnapshotTree(t, disk, trieDB, newRoot)
	synctest.RequireRootRetired(t, tree, oldRoot)

	client := serve(t, ctx, trieDB, WithSnapshot(tree))
	got, more, err := client.FetchLeaves(ctx, LeafRange{Root: oldRoot, Limit: numAccounts})
	require.NoError(t, err)
	require.Equal(t, keys, got.Keys)
	require.Equal(t, vals, got.Vals)
	require.False(t, more)

	// Without this the assertions above pass on a pure trie fallback.
	require.Len(t, tree.Reads(), 1)
}

func newLeafResponder(tb testing.TB, trieDB *triedb.Database, opts ...HandlerOption) *responder {
	tb.Helper()
	return newResponder(loggingtest.New(tb, logging.Debug), trieDB, common.HashLength, opts...)
}
