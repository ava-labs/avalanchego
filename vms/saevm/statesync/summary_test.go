package statesync

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// FuzzSummary checks round-trip encoding.
func FuzzSummary(f *testing.F) {
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
		require.Equal(t, summary.Height(), parsedSummary.Height(), "Height()")
		require.Equal(t, summary.ID(), parsedSummary.ID(), "ID()")
	})
}
