// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func FuzzSummaryRoundTrip(f *testing.F) {
	f.Add(uint64(0), []byte{})
	f.Add(uint64(1), []byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, height uint64, hashBytes []byte) {
		hash := common.BytesToHash(hashBytes)
		summary := &Summary{
			height:    height,
			blockHash: hash,
		}
		summaryBytes := summary.Bytes()

		parsedSummary, err := (*SummaryHandler)(nil).ParseStateSummary(t.Context(), summaryBytes)
		require.NoError(t, err, "ParseStateSummary()")
		if diff := cmp.Diff(summary, parsedSummary, CmpOpt()); diff != "" {
			t.Errorf("ParseStateSummary() mismatch (-want +got):\n%s", diff)
		}
		require.Equalf(t, summary.ID(), parsedSummary.ID(), "%T.ID()", summary)
	})
}

// FuzzSummaryID ensures the ID is sensitive to any changes in the summary's fields.
func FuzzSummaryID(f *testing.F) {
	f.Add(uint64(1), []byte{1, 2, 3})

	f.Fuzz(func(t *testing.T, height uint64, hashBytes []byte) {
		hash := common.BytesToHash(hashBytes)
		baseID := NewSummary(hash, height).ID()

		// Changing the blockHash must change the ID.
		alteredHash := hash
		alteredHash[0] ^= 1
		changedHashID := NewSummary(alteredHash, height).ID()
		require.NotEqual(t, baseID, changedHashID, "ID() must change when blockHash changes")

		// Changing the height must change the ID.
		alteredHeight := height ^ 1
		changedHeightID := NewSummary(hash, alteredHeight).ID()
		require.NotEqual(t, baseID, changedHeightID, "ID() must change when height changes")
	})
}
