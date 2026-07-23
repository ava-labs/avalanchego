// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func FuzzSummaryRoundTrip(f *testing.F) {
	f.Add(uint64(0), []byte{})
	f.Add(uint64(1), []byte{1, 2, 3})

	handler, err := New(Config{}, rawdb.NewMemoryDatabase(), logging.NoLog{})
	require.NoError(f, err, "New()")
	f.Fuzz(func(t *testing.T, height uint64, hashBytes []byte) {
		summary := NewSummary(common.BytesToHash(hashBytes), height)
		summaryBytes := summary.Bytes()

		parsedSummary, err := handler.ParseStateSummary(t.Context(), summaryBytes)
		require.NoError(t, err, "ParseStateSummary()")
		if diff := cmp.Diff(summary, parsedSummary, CmpOpt()); diff != "" {
			t.Errorf("ParseStateSummary() mismatch (-want +got):\n%s", diff)
		}
		require.Equalf(t, summary.ID(), parsedSummary.ID(), "%T.ID()", summary)
	})
}

// FuzzSummaryID ensures the ID is sensitive to any changes in the summary's
// fields.
func FuzzSummaryID(f *testing.F) {
	f.Add(
		uint64(1), []byte{1, 2, 3},
		uint64(2), []byte{1, 2, 3},
	)
	f.Fuzz(func(t *testing.T,
		height1 uint64, hashBytes1 []byte,
		height2 uint64, hashBytes2 []byte,
	) {
		summary1 := NewSummary(common.BytesToHash(hashBytes1), height1)
		summary2 := NewSummary(common.BytesToHash(hashBytes2), height2)
		if diff := cmp.Diff(summary1, summary2, CmpOpt()); diff != "" {
			require.NotEqual(t, summary1.ID(), summary2.ID(), "Hash collision!")
		}
	})
}
