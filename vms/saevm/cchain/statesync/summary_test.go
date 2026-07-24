// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var summaryCmpOpts = cmp.Options{
	cmp.AllowUnexported(summary{}),
	cmpopts.IgnoreFields(summary{}, "canotoData"),
	statesync.CmpOpt(),
}

// newSummary returns a [summary] wrapping the block at the given height and
// hash, paired with the C-Chain atomic trie root at that height.
func newSummary(blockHash, root common.Hash, height uint64) *summary {
	return &summary{
		summary:     *statesync.NewSummary(blockHash, height),
		settledRoot: root,
	}
}

// FuzzSummaryRoundTrip checks round-trip encoding.
func FuzzSummaryRoundTrip(f *testing.F) {
	f.Add(uint64(0), []byte{}, []byte{})
	f.Add(uint64(1), []byte{1, 2, 3}, []byte{4, 5, 6})

	f.Fuzz(func(t *testing.T, height uint64, hashBytes, rootBytes []byte) {
		summary := newSummary(
			common.BytesToHash(hashBytes),
			common.BytesToHash(rootBytes),
			height,
		)

		parsed, err := (&SummaryHandler{}).ParseStateSummary(t.Context(), summary.Bytes())
		require.NoError(t, err, "parseSummary()")
		if diff := cmp.Diff(summary, parsed, summaryCmpOpts); diff != "" {
			t.Errorf("Summary mismatch (-want +got):\n%s", diff)
		}
		require.Equalf(t, summary.ID(), parsed.ID(), "%T.ID()", summary)
	})
}

// FuzzSummaryID ensures the ID is sensitive to any changes in the summary's
// fields.
func FuzzSummaryID(f *testing.F) {
	f.Add(uint64(1), []byte{1, 2, 3}, []byte{4, 5, 6})

	f.Fuzz(func(t *testing.T, height uint64, hashBytes, rootBytes []byte) {
		if len(hashBytes) == 0 || len(rootBytes) == 0 {
			t.Skip("hashBytes and rootBytes must be non-empty to test ID sensitivity")
		}

		hash := common.BytesToHash(hashBytes)
		root := common.BytesToHash(rootBytes)
		baseID := newSummary(hash, root, height).ID()

		// Changing the blockHash must change the ID.
		alteredHash := hash
		alteredHash[0] ^= 1
		require.NotEqual(t, baseID, newSummary(alteredHash, root, height).ID(), "blockHash changes")

		// Changing the height must change the ID.
		require.NotEqual(t, baseID, newSummary(hash, root, height^1).ID(), "height changes")

		// Changing the C-Chain trie root must change the ID.
		alteredRoot := root
		alteredRoot[0] ^= 1
		require.NotEqual(t, baseID, newSummary(hash, alteredRoot, height).ID(), "root changes")
	})
}
