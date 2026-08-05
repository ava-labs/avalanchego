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

	h := new(SummaryHandler)
	f.Fuzz(func(t *testing.T, height uint64, hashBytes, rootBytes []byte) {
		summary := newSummary(
			common.BytesToHash(hashBytes),
			common.BytesToHash(rootBytes),
			height,
		)

		parsed, err := h.ParseStateSummary(t.Context(), summary.Bytes())
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
	f.Add(
		uint64(1), []byte{1, 2, 3}, []byte{4, 5, 6},
		uint64(2), []byte{1, 2, 3}, []byte{4, 5, 6},
	)
	f.Fuzz(func(t *testing.T,
		height1 uint64, hashBytes1, rootBytes1 []byte,
		height2 uint64, hashBytes2, rootBytes2 []byte,
	) {
		summary1 := newSummary(
			common.BytesToHash(hashBytes1),
			common.BytesToHash(rootBytes1),
			height1,
		)
		summary2 := newSummary(
			common.BytesToHash(hashBytes2),
			common.BytesToHash(rootBytes2),
			height2,
		)
		if diff := cmp.Diff(summary1, summary2, summaryCmpOpts); diff != "" {
			require.NotEqual(t, summary1.ID(), summary2.ID(), "Hash collision!")
		}
	})
}
