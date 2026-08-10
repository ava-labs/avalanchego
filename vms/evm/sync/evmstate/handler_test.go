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
			name: "zero KeyLimit",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 0,
			},
		},
		{
			name: "KeyLimit overflows uint16",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: math.MaxUint16 + 1,
			},
		},
		{
			name: "StartKey > EndKey",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: bytes.Repeat([]byte{0xff}, common.HashLength),
				EndKey:   bytes.Repeat([]byte{0x00}, common.HashLength),
				KeyLimit: 10,
			},
		},
		{
			name: "StartKey wrong length",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: []byte{0x01, 0x02},
				KeyLimit: 10,
			},
		},
		{
			name: "RootHash empty",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{}.Bytes(),
				KeyLimit: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength)
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
		{name: "whole trie has no proof", limit: numKeys},
		{name: "partial range includes proof", limit: numKeys / 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, numKeys)

			r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength)
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
		{name: "missing root", limit: numKeys, badRoot: true, wantErr: errRootNotFound},
		// A corrupt trie fails the proof step, a server fault.
		{name: "corrupted trie", limit: numKeys / 2, corruptTrie: true, wantErr: p2p.ErrUnexpected},
		{name: "cancelled context", limit: numKeys, cancelCtx: true, wantErr: errServingCancelled},
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

			// A server fault logs at ERROR, which loggingtest.New turns into a
			// failure, so record instead and assert on what was logged.
			log := loggingtest.NewRecorder(logging.Debug)
			r := newResponder(log, trieDB, common.HashLength)
			resp, appErr := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: rootHash,
				KeyLimit: tt.limit,
			})
			require.ErrorIs(t, appErr, tt.wantErr)
			require.Nil(t, resp)

			// Only a fault is worth an ERROR, a peer's bad request is not.
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
		name  string
		apply func(snapshotCase)
	}{
		{name: "mirrors the trie", apply: func(snapshotCase) {}},
		{name: "corrupt middle segment", apply: func(c snapshotCase) { c.corrupt(64, 128) }},
		// The only shape where the gap fill advances past the snapshot index,
		// which is where the segment trim bites.
		{name: "missing leaves", apply: func(c snapshotCase) {
			kept := make([]synctest.StaticPair, 0, len(c.leaves))
			kept = append(kept, c.leaves[:1]...)
			kept = append(kept, c.leaves[100:]...)
			c.snap.Accounts = kept
		}},
	}

	for _, limit := range []uint32{1, 63, 64, 65, 129, 200} {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("limit=%d/%s", limit, shape.name), func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := newAccountCase(t, trieDB, numAccounts)
				shape.apply(c)

				r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, WithSnapshot(c.snap))
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

func TestResponder_ReadsSnapshotAtDiskRoot(t *testing.T) {
	t.Parallel()

	for _, kind := range snapshotKinds {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			c := kind.build(t, trieDB, 20)

			r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, WithSnapshot(c.snap))
			requireServesWholeTrie(t, r, c)

			reads := c.snap.Reads()
			require.Len(t, reads, 1)
			require.Equal(t, c.snap.DiskRoot(), reads[0].Root, "must read the disk layer")
			require.NotEqual(t, c.root, reads[0].Root, "must not read at the requested root")
			require.Equal(t, c.account, reads[0].Account, "must read the requested scope")
		})
	}
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
			r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, opts...)
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
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, int(MaxLeavesLimit)+200)

	r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength)
	resp, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(MaxLeavesLimit) + 200,
	})
	require.Nil(t, appErr)
	require.NotNil(t, resp)
	require.Len(t, resp.Keys, int(MaxLeavesLimit))
}

// snapshotKinds is the leaf scopes every snapshot path must serve.
var snapshotKinds = []struct {
	name  string
	build func(*testing.T, *triedb.Database, int) snapshotCase
}{
	{name: "account", build: newAccountCase},
	{name: "storage", build: newStorageCase},
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
	return snapshotCase{
		root:    root,
		account: account,
		keys:    keys,
		vals:    vals,
		snap:    &synctest.StaticSnapshot{Storage: map[common.Hash][]synctest.StaticPair{account: leaves}},
		leaves:  leaves,
	}
}

func TestResponder_Snapshot(t *testing.T) {
	t.Parallel()

	// 130 leaves spans three snapshotSegmentLen segments.
	const numLeaves = 130

	tests := []struct {
		name string
		// corruptFrom/corruptTo desync those leaves, err fails the snapshot.
		corruptFrom int
		corruptTo   int
		err         bool
	}{
		{name: "fast path serves leaves"},
		{name: "slow path bridges an invalid middle segment", corruptFrom: 64, corruptTo: 128},
		{name: "invalid head segment", corruptFrom: 0, corruptTo: 64},
		{name: "invalid tail segment", corruptFrom: 128, corruptTo: numLeaves},
		{name: "invalid segment boundary", corruptFrom: 63, corruptTo: 65},
		{name: "all invalid falls back to trie", corruptFrom: 0, corruptTo: numLeaves},
		{name: "unavailable snapshot falls back to trie", err: true},
	}

	for _, kind := range snapshotKinds {
		for _, tt := range tests {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				trieDB := synctest.NewTrieDB()
				c := kind.build(t, trieDB, numLeaves)

				c.corrupt(tt.corruptFrom, tt.corruptTo)
				if tt.err {
					c.snap.Err = errors.New("snapshot unavailable")
				}

				r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength, WithSnapshot(c.snap))
				requireServesWholeTrie(t, r, c)

				// The trie fallback serves the same leaves, so this is what
				// proves the snapshot ran at all.
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
