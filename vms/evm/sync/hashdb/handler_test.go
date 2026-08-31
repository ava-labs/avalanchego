// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"errors"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		"errInvalidRoot":            errInvalidRoot,
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
		snapshot *synctest.Snapshot
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

// However the snapshot diverges from the trie, corrupt values, dropped runs,
// or reads that fail outright, the fill MUST produce exactly the trie's
// leaves and report whether the trie holds more.
func TestFillFromSnapshot(t *testing.T) {
	t.Parallel()

	const numLeaves = 7 * segmentLen
	trieDB := synctest.NewTrieDB()
	root, keys, vals, snap := synctest.FillAccountTrie(t, trieDB, numLeaves)
	tr, err := trie.New(trie.TrieID(root), trieDB)
	require.NoError(t, err)

	storageRoot, storageKeys, storageVals := synctest.FillTrie(t, trieDB, numLeaves)
	storageTrie, err := trie.New(trie.TrieID(storageRoot), trieDB)
	require.NoError(t, err)
	storageAccount := common.Hash{0xaa}
	storagePairs := make([]synctest.Pair, numLeaves)
	for i := range storagePairs {
		storagePairs[i] = synctest.Pair{
			K: storageKeys[i],
			V: storageVals[i],
		}
	}
	snap.Storage = map[common.Hash][]synctest.Pair{
		storageAccount: storagePairs,
	}

	// corrupt replaces the all the values from the pairs with the first value.
	// It MUST produce well-formed values to avoid skipping snapshot iteration
	// entirely.
	corrupt := func(pairs []synctest.Pair) []synctest.Pair {
		out := slices.Clone(pairs)
		for i := range out {
			out[i].V = out[0].V
		}
		return out
	}

	// The account snapshot has various inaccuracies and missing values:
	const dropTo = 5*segmentLen + segmentLen/2
	accounts := snap.Accounts
	snap.Accounts = slices.Concat(
		accounts[:segmentLen],                            // [0, 64)    valid
		corrupt(accounts[segmentLen:2*segmentLen]),       // [64, 128)  corrupt
		accounts[2*segmentLen:3*segmentLen],              // [128, 192) valid
		corrupt(accounts[3*segmentLen:4*segmentLen]),     // [192, 256) corrupt
		accounts[4*segmentLen:4*segmentLen+segmentLen/2], // [256, 288) valid
		[]synctest.Pair{},                                // [288, 320) missing
		accounts[dropTo:],                                // [320, 384) valid
	)

	minKey := make([]byte, common.HashLength)
	const earlyEnd = numLeaves - segmentLen/2
	tests := []struct {
		name     string
		snapshot trieSnapshot
		trie     *trie.Trie
		r        *leafRange
		openErr  error
		iterErr  error
		want     Leaves
		wantMore bool
	}{
		{
			name:     "bridges_every_divergence",
			snapshot: accountSnapshot{s: snap},
			trie:     tr,
			r:        newLeafRange(minKey, earlyEnd),
			want:     Leaves{Keys: keys[:earlyEnd], Vals: vals[:earlyEnd]},
			wantMore: true,
		},
		{
			name:     "clean_tail_proves_in_one_shot",
			snapshot: accountSnapshot{s: snap},
			trie:     tr,
			r:        newLeafRange(keys[dropTo], numLeaves),
			want:     Leaves{Keys: keys[dropTo:], Vals: vals[dropTo:]},
		},
		{
			name:     "storage_trie_proves_in_one_shot",
			snapshot: storageSnapshot{s: snap, account: storageAccount},
			trie:     storageTrie,
			r:        newLeafRange(minKey, numLeaves),
			want:     Leaves{Keys: storageKeys, Vals: storageVals},
		},
		{
			name:     "unopenable_snapshot_fills_nothing",
			snapshot: accountSnapshot{s: snap},
			trie:     tr,
			r:        newLeafRange(minKey, numLeaves),
			openErr:  errors.New("snapshot unavailable"),
			wantMore: true,
		},
		{
			name:     "failing_iterator_fills_nothing",
			snapshot: storageSnapshot{s: snap, account: storageAccount},
			trie:     storageTrie,
			r:        newLeafRange(minKey, numLeaves),
			iterErr:  errors.New("iteration failed"),
			wantMore: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snap.OpenErr = test.openErr
			snap.IterErr = test.iterErr

			more, err := fillFromSnapshot(test.snapshot, test.trie, test.r)
			require.NoError(t, err)
			got := Leaves{Keys: test.r.keys, Vals: test.r.vals}
			require.Empty(t, cmp.Diff(test.want, got, cmpopts.EquateEmpty()))
			require.Equal(t, test.wantMore, more)
		})
	}
}
