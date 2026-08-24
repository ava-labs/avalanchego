// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// Snapshot fixture shapes, derived from snapshotSegmentLen so they survive a
// change to it.
const (
	oneSegment  = snapshotSegmentLen
	twoSegments = 2 * snapshotSegmentLen
	// segmentedLeaves fills two segments and leaves a short third, so a tail
	// case spans fewer leaves than a whole segment.
	segmentedLeaves = twoSegments + 2
)

func TestErrorSentinels(t *testing.T) {
	synctest.RequireDistinctAppErrors(t, map[string]*avacommon.AppError{
		"errInvalidRequest":   errInvalidRequest,
		"errRootNotFound":     errRootNotFound,
		"errServingCancelled": errServingCancelled,
	})
}

func TestResponder_ValidationRejects(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	tests := []struct {
		name string
		req  *syncpb.GetLeafRequest
	}{
		{
			name: "zero_key_limit",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 0,
			},
		},
		{
			name: "start_key_after_end_key",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: bytes.Repeat([]byte{0xff}, common.HashLength),
				EndKey:   bytes.Repeat([]byte{0x00}, common.HashLength),
				KeyLimit: 10,
			},
		},
		{
			name: "start_key_wrong_length",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: []byte{0x01, 0x02},
				KeyLimit: 10,
			},
		},
		{
			name: "root_hash_empty",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{}.Bytes(),
				KeyLimit: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLeafResponder(t, trieDB)
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), tt.req)
			require.ErrorIs(t, appErr, errInvalidRequest)
			require.Nil(t, resp)
		})
	}
}

func TestResponder_Serves(t *testing.T) {
	t.Parallel()

	const numKeys = 50

	tests := []struct {
		name  string
		limit uint32
	}{
		{
			name:  "whole_trie_has_no_proof",
			limit: numKeys,
		},
		{
			name:  "partial_range_includes_proof",
			limit: numKeys / 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, numKeys)

			r := newLeafResponder(t, trieDB)
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: tt.limit,
			})
			require.Nil(t, appErr)
			require.NotNil(t, resp)

			n := int(tt.limit)
			require.Equal(t, keys[:n], resp.Keys)
			require.Equal(t, vals[:n], resp.Values)
			if n < numKeys {
				require.NotEmpty(t, resp.ProofVals)
			} else {
				require.Empty(t, resp.ProofVals)
			}
		})
	}
}

func TestResponder_Rejects(t *testing.T) {
	t.Parallel()

	const numKeys = 50

	tests := []struct {
		name        string
		limit       uint32
		badRoot     bool
		corruptTrie bool
		cancelCtx   bool
		wantErr     *avacommon.AppError
	}{
		{
			name:    "missing_root",
			limit:   numKeys,
			badRoot: true,
			wantErr: errRootNotFound,
		},
		// A corrupt trie fails the proof step, a server fault.
		{
			name:        "corrupted_trie",
			limit:       numKeys / 2,
			corruptTrie: true,
			wantErr:     p2p.ErrUnexpected,
		},
		{
			name:      "cancelled_context",
			limit:     numKeys,
			cancelCtx: true,
			wantErr:   errServingCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB, disk := synctest.NewTrieDBWithDisk()
			root, _, _ := synctest.FillTrie(t, trieDB, numKeys)

			if tt.corruptTrie {
				tr, err := trie.New(trie.TrieID(root), trieDB)
				require.NoError(t, err)
				synctest.CorruptTrie(t, disk, tr, 2)
			}
			rootHash := root.Bytes()
			if tt.badRoot {
				rootHash = bytes.Repeat([]byte{0xab}, common.HashLength)
			}
			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			// loggingtest.New fails the test on an ERROR, so record instead.
			log := loggingtest.NewRecorder(logging.Debug)
			r := newResponder(log, trieDB, common.HashLength)
			resp, appErr := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: rootHash,
				KeyLimit: tt.limit,
			})
			require.ErrorIs(t, appErr, tt.wantErr)
			require.Nil(t, resp)

			// Only a fault earns an ERROR, a peer's bad request does not.
			faults := log.AtLeast(logging.Error)
			if tt.wantErr == p2p.ErrUnexpected {
				require.Len(t, faults, 1, "a server fault must be logged")
			} else {
				require.Empty(t, faults, "rejecting a peer must not log an error")
			}
		})
	}
}

func TestResponder_HonorsKeyLimit(t *testing.T) {
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
			name:        "corrupt_middle_segment",
			corruptFrom: oneSegment,
			corruptTo:   twoSegments,
		},
		// The only shape where the segment trim bites.
		{
			name:     "missing_leaves",
			dropFrom: 1,
			dropTo:   100,
		},
	}

	for _, limit := range []uint32{1, 63, 64, 65, 129, 200} {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("limit=%d/%s", limit, shape.name), func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := newAccountCase(t, trieDB, numAccounts)
				c.corrupt(shape.corruptFrom, shape.corruptTo)
				c.drop(shape.dropFrom, shape.dropTo)

				r := newLeafResponder(t, trieDB, WithSnapshot(c.snap))
				resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
					RootHash: c.root.Bytes(),
					KeyLimit: limit,
				})
				require.Nil(t, appErr)
				require.NotNil(t, resp)
				require.LessOrEqual(t, len(resp.Keys), int(limit))
				require.Len(t, resp.Values, len(resp.Keys))
			})
		}
	}
}

// The snapshot is a latency optimisation, never visible to the peer.
func TestResponder_SnapshotChangesNothing(t *testing.T) {
	t.Parallel()

	const numAccounts = 300

	// One limit past a segment boundary, so every divergence is reachable and
	// the response is trimmed. TestResponder_HonorsKeyLimit sweeps the limits.
	const limit = uint32(oneSegment + 1)

	divergences := map[string][2]int{
		"mirrors_the_trie": {0, 0},
		"head_segment":     {0, oneSegment},
		"middle_segment":   {oneSegment, twoSegments},
		"segment_boundary": {oneSegment - 1, oneSegment + 1},
		"tail_segment":     {numAccounts - oneSegment, numAccounts},
		"every_segment":    {0, numAccounts},
	}

	for name, diverge := range divergences {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			trieDB := synctest.NewTrieDB()
			c := newAccountCase(t, trieDB, numAccounts)
			c.corrupt(diverge[0], diverge[1])
			withSnap := newLeafResponder(t, trieDB, WithSnapshot(c.snap))

			bareDB := synctest.NewTrieDB()
			bare := newAccountCase(t, bareDB, numAccounts)
			require.Equal(t, c.root, bare.root)
			noSnap := newResponder(loggingtest.New(t, logging.Debug), bareDB, common.HashLength)

			req := func() *syncpb.GetLeafRequest {
				return &syncpb.GetLeafRequest{RootHash: c.root.Bytes(), KeyLimit: limit}
			}
			got, gotErr := withSnap.Respond(t.Context(), ids.GenerateTestNodeID(), req())
			want, wantErr := noSnap.Respond(t.Context(), ids.GenerateTestNodeID(), req())

			require.Equal(t, wantErr, gotErr)
			require.Equal(t, want.GetKeys(), got.GetKeys())
			require.Equal(t, want.GetValues(), got.GetValues())
			require.Equal(t, len(want.GetProofVals()) == 0, len(got.GetProofVals()) == 0,
				"proof presence must not depend on the snapshot")
		})
	}
}

// The whole-trie shortcut's complement: without a proof a client reads a short
// range as the trie's end.
func TestResponder_PartialResponseCarriesProof(t *testing.T) {
	t.Parallel()

	const numAccounts = 300

	for _, limit := range []uint32{251, 260, 299} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			c := newAccountCase(t, trieDB, numAccounts)

			// The bridge then overshoots and the limit trims the segment that
			// reaches the trie's end.
			c.drop(1, 50)

			r := newLeafResponder(t, trieDB, WithSnapshot(c.snap))
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: c.root.Bytes(),
				KeyLimit: limit,
			})
			require.Nil(t, appErr)
			require.NotNil(t, resp)
			require.Less(t, len(resp.Keys), numAccounts, "the limit must leave leaves unserved")
			require.NotEmpty(t, resp.ProofVals, "a partial response must carry a proof")
		})
	}
}

func TestResponder_ReadsSnapshotAtDiskRoot(t *testing.T) {
	t.Parallel()

	for _, kind := range snapshotKinds {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			c := kind.build(t, trieDB, 20)

			r := newLeafResponder(t, trieDB, WithSnapshot(c.snap))
			requireServesWholeTrie(t, r, c)

			reads := c.snap.Reads()
			require.Len(t, reads, 1)
			require.Equal(t, c.snap.DiskRoot(), reads[0].Root, "must read the disk layer")
			require.NotEqual(t, c.root, reads[0].Root, "must not read at the requested root")
			require.Equal(t, c.account, reads[0].Account, "must read the requested scope")
		})
	}
}

// A real [snapshot.Tree] driven through the case the disk-layer read exists
// for: a root the tree has retired.
func TestResponder_ServesHistoricalRootFromDiskLayer(t *testing.T) {
	t.Parallel()

	const numAccounts = 100

	trieDB, disk := synctest.NewTrieDBWithDisk()
	oldRoot, keys, vals, _ := synctest.FillAccountTrie(t, trieDB, numAccounts)
	newRoot := synctest.AdvanceAccountTrie(t, trieDB, oldRoot, 30)
	require.NotEqual(t, oldRoot, newRoot)

	tree := synctest.NewSnapshotTree(t, disk, trieDB, newRoot)
	synctest.RequireRootRetired(t, tree, oldRoot)

	r := newLeafResponder(t, trieDB, WithSnapshot(tree))
	resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: oldRoot.Bytes(),
		KeyLimit: uint32(len(keys)),
	})
	require.Nil(t, appErr)
	require.NotNil(t, resp)

	require.Equal(t, keys, resp.Keys)
	require.Equal(t, vals, resp.Values)

	// Without this the assertions above pass on a pure trie fallback.
	reads := tree.Reads()
	require.Len(t, reads, 1)
	require.Equal(t, tree.DiskRoot(), reads[0].Root, "the disk layer must have been read")
}

func TestResponder_BoundedRange(t *testing.T) {
	t.Parallel()

	// The snapshot applies its own endKey cut, so both paths are checked.
	for _, withSnapshot := range []bool{false, true} {
		t.Run(fmt.Sprintf("snapshot=%t", withSnapshot), func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			c := newAccountCase(t, trieDB, 50)

			var opts []HandlerOption
			if withSnapshot {
				opts = append(opts, WithSnapshot(c.snap))
			}
			r := newLeafResponder(t, trieDB, opts...)
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: c.root.Bytes(),
				StartKey: c.keys[10],
				EndKey:   c.keys[30],
				KeyLimit: uint32(len(c.keys)),
			})
			require.Nil(t, appErr)
			require.NotNil(t, resp)
			// EndKey is inclusive.
			require.Equal(t, c.keys[10:31], resp.Keys)
			require.Equal(t, c.vals[10:31], resp.Values)
			require.NotEmpty(t, resp.ProofVals)
		})
	}
}

func TestResponder_CapsAtMaxLeavesLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		keyLimit uint32
	}{
		{
			name:     "over_the_limit",
			keyLimit: uint32(MaxLeavesLimit) + 200,
		},
		// Capping in uint16 would truncate this to 4 rather than clamp it.
		{
			name:     "overflows_uint16",
			keyLimit: math.MaxUint16 + 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, _, _ := synctest.FillTrie(t, trieDB, int(MaxLeavesLimit)+200)

			r := newLeafResponder(t, trieDB)
			resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: tt.keyLimit,
			})
			require.Nil(t, appErr)
			require.NotNil(t, resp)
			require.Len(t, resp.Keys, int(MaxLeavesLimit))
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
}

// snapshotCase is a trie plus a snapshot mirroring it, for one kind of leaf.
type snapshotCase struct {
	root    common.Hash
	account common.Hash // zero for the account trie
	keys    [][]byte
	vals    [][]byte
	snap    *synctest.StaticSnapshot
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

func newAccountCase(t *testing.T, trieDB *triedb.Database, n int) snapshotCase {
	root, keys, vals, snap := synctest.FillAccountTrie(t, trieDB, n)
	return snapshotCase{root: root, keys: keys, vals: vals, snap: snap, leaves: snap.Accounts}
}

func newStorageCase(t *testing.T, trieDB *triedb.Database, n int) snapshotCase {
	account := common.HexToHash("0xa11ce")
	root, keys, vals := synctest.FillTrie(t, trieDB, n)

	// Storage slots are already trie-encoded, so the snapshot mirrors the trie.
	leaves := make([]synctest.StaticPair, len(keys))
	for i := range keys {
		leaves[i] = synctest.StaticPair{K: keys[i], V: vals[i]}
	}
	static := &synctest.StaticSnapshot{Storage: map[common.Hash][]synctest.StaticPair{account: leaves}}
	return snapshotCase{
		root:    root,
		account: account,
		keys:    keys,
		vals:    vals,
		snap:    static,
		leaves:  leaves,
	}
}

func TestResponder_Snapshot(t *testing.T) {
	t.Parallel()

	const numLeaves = segmentedLeaves

	tests := []struct {
		name string
		// corruptFrom/corruptTo desync those leaves, err fails the snapshot.
		corruptFrom int
		corruptTo   int
		err         bool
	}{
		{
			name: "fast_path_serves_leaves",
		},
		{
			name:        "slow_path_bridges_an_invalid_middle_segment",
			corruptFrom: oneSegment,
			corruptTo:   twoSegments,
		},
		{
			name:        "invalid_head_segment",
			corruptFrom: 0,
			corruptTo:   oneSegment,
		},
		{
			name:        "invalid_tail_segment",
			corruptFrom: twoSegments,
			corruptTo:   numLeaves,
		},
		{
			name:        "invalid_segment_boundary",
			corruptFrom: oneSegment - 1,
			corruptTo:   oneSegment + 1,
		},
		{
			name:        "all_invalid_falls_back_to_trie",
			corruptFrom: 0,
			corruptTo:   numLeaves,
		},
		{
			name: "unavailable_snapshot_falls_back_to_trie",
			err:  true,
		},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)

				c.corrupt(tt.corruptFrom, tt.corruptTo)
				if tt.err {
					c.snap.OpenErr = errors.New("snapshot unavailable")
				}

				r := newLeafResponder(t, trieDB, WithSnapshot(c.snap))
				requireServesWholeTrie(t, r, c)

				// The trie fallback serves the same leaves, so only this proves
				// the snapshot ran.
				require.NotEmpty(t, c.snap.Reads(), "snapshot must be consulted")
			})
		}
	}
}

// requireServesWholeTrie asserts a whole-trie request for c returns its leaves.
func requireServesWholeTrie(t *testing.T, r *responder, c snapshotCase) {
	t.Helper()
	resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash:    c.root.Bytes(),
		AccountHash: accountBytes(c.account),
		KeyLimit:    uint32(len(c.keys)),
	})
	require.Nil(t, appErr)
	require.NotNil(t, resp)
	require.Equal(t, c.keys, resp.Keys)
	require.Equal(t, c.vals, resp.Values)
}

// newSnapshotQuery opens a query over c's snapshot. The trie fallback hides the
// snapshot from a response assertion, so these tests read it directly.
func newSnapshotQuery(t *testing.T, trieDB *triedb.Database, c snapshotCase, keyLimit int, endKey []byte) *query {
	t.Helper()
	r := newLeafResponder(t, trieDB, WithSnapshot(c.snap))
	q, appErr := newQuery(r, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash:    c.root.Bytes(),
		AccountHash: accountBytes(c.account),
		EndKey:      endKey,
		KeyLimit:    uint32(keyLimit),
	})
	require.Nil(t, appErr)
	return q
}

func TestQuery_ReadsSnapshotLeaves(t *testing.T) {
	t.Parallel()

	const numLeaves = 20

	tests := []struct {
		name string
		// keyLimit and endAt bound the read, endAt indexing the last leaf wanted.
		keyLimit int
		endAt    int
		iterErr  bool
		wantLen  int
	}{
		{
			name:     "every_leaf",
			keyLimit: numLeaves,
			wantLen:  numLeaves,
		},
		{
			name:     "key_limit_truncates",
			keyLimit: 5,
			wantLen:  5,
		},
		{
			name:     "end_key_truncates",
			keyLimit: numLeaves,
			endAt:    8,
			wantLen:  8,
		},
		{
			name:     "iteration_failure_reads_nothing",
			keyLimit: numLeaves,
			iterErr:  true,
		},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)
				if tt.iterErr {
					c.snap.IterErr = errors.New("iteration failed")
				}

				var endKey []byte
				if tt.endAt > 0 {
					endKey = c.keys[tt.endAt-1]
				}

				q := newSnapshotQuery(t, trieDB, c, tt.keyLimit, endKey)
				keys, vals := q.readFromSnapshot(t.Context())

				if tt.wantLen == 0 {
					require.Empty(t, keys)
					require.Empty(t, vals)
					return
				}
				require.Equal(t, c.keys[:tt.wantLen], keys)
				require.Equal(t, c.vals[:tt.wantLen], vals, "snapshot values must equal the trie leaves")
			})
		}
	}
}

func TestQuery_SnapshotFillsResponse(t *testing.T) {
	t.Parallel()

	const numLeaves = segmentedLeaves

	tests := []struct {
		name        string
		corruptFrom int
		corruptTo   int
	}{
		{
			name: "whole_range_at_once",
		},
		{
			name:        "bridged_middle_segment",
			corruptFrom: oneSegment,
			corruptTo:   twoSegments,
		},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)
				c.corrupt(tt.corruptFrom, tt.corruptTo)

				q := newSnapshotQuery(t, trieDB, c, numLeaves, nil)
				done, err := q.fillFromSnapshot(t.Context())
				require.NoError(t, err)

				require.True(t, done, "the snapshot must satisfy a whole-trie request")
				require.Equal(t, c.keys, q.resp.Keys)
				require.Equal(t, c.vals, q.resp.Values)
			})
		}
	}
}

func TestRegisterHandler_ServesOverNetwork(t *testing.T) {
	t.Parallel()

	const numLeaves = 50

	tests := []struct {
		name string
		// startAt indexes the first leaf wanted, 0 leaves the start key unset.
		startAt   int
		keyLimit  uint32
		wantLen   int
		wantProof bool
		wantMore  bool
		snapshot  bool
	}{
		{
			name:     "whole_trie_carries_no_proof",
			keyLimit: numLeaves,
			wantLen:  numLeaves,
		},
		{
			name:      "partial_range_carries_a_proof",
			keyLimit:  20,
			wantLen:   20,
			wantProof: true,
			wantMore:  true,
		},
		// Only a range that starts at the trie's head may omit the proof, so
		// reaching the end from a start key does not make the root sufficient.
		{
			name:      "range_to_the_end_still_carries_a_proof",
			startAt:   10,
			keyLimit:  numLeaves,
			wantLen:   numLeaves - 10,
			wantProof: true,
		},
		{
			name:      "partial_range_from_a_snapshot_carries_a_proof",
			keyLimit:  20,
			wantLen:   20,
			wantProof: true,
			wantMore:  true,
			snapshot:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			trieDB := synctest.NewTrieDB()

			c := newAccountCase(t, trieDB, numLeaves)
			root, keys, vals := c.root, c.keys, c.vals

			var opts []HandlerOption
			if tt.snapshot {
				opts = append(opts, WithSnapshot(c.snap))
			}

			firstKey := bytes.Repeat([]byte{0x00}, common.HashLength)
			var startKey []byte
			if tt.startAt > 0 {
				startKey = keys[tt.startAt]
				firstKey = startKey
			}

			// The wire response carries the proof, which the verified client
			// consumes rather than returns, so this asserts at the transport.
			resp := rawResponse(t, ctx, serve(t, ctx, trieDB, opts...), &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: startKey,
				KeyLimit: tt.keyLimit,
			})

			to := tt.startAt + tt.wantLen
			require.Equal(t, keys[tt.startAt:to], resp.GetKeys())
			require.Equal(t, vals[tt.startAt:to], resp.GetValues())
			require.Equal(t, tt.wantProof, len(resp.GetProofVals()) > 0, "proof presence")

			// libevm is the oracle for the proof the handler emits.
			more, err := trie.VerifyRangeProof(root, firstKey,
				resp.GetKeys(), resp.GetValues(), proofFrom(t, resp.GetProofVals()))
			require.NoError(t, err)
			require.Equal(t, tt.wantMore, more)

			if tt.snapshot {
				// A read is recorded when the iterator is opened, so this shows
				// the snapshot was consulted, not that it served the leaves.
				require.NotEmpty(t, c.snap.Reads(), "the snapshot must be consulted")
			}
		})
	}
}

// proofFrom rebuilds proof nodes keyed by hash. Nil for a whole-trie response,
// which VerifyRangeProof then checks against the root alone.
func proofFrom(t *testing.T, vals [][]byte) ethdb.Database {
	t.Helper()
	if len(vals) == 0 {
		return nil
	}
	db := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, v := range vals {
		require.NoError(t, db.Put(crypto.Keccak256(v), v))
	}
	return db
}

func newLeafResponder(tb testing.TB, trieDB *triedb.Database, opts ...HandlerOption) *responder {
	tb.Helper()
	return newResponder(loggingtest.New(tb, logging.Debug), trieDB, common.HashLength, opts...)
}
