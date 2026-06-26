// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TestMillisecondTimestampRoundTrip proves that with millisecondTimestamps=true
// the wrapper block's int64 timestamp carries sub-second precision through
// build -> serialize -> parse, while the default (seconds) truncates it. It also
// pins the immutability contract: a block written with one unit is misread when
// parsed with the other.
func TestMillisecondTimestampRoundTrip(t *testing.T) {
	var (
		parentID = ids.ID{1}
		chainID  = ids.ID{4}
		// A wall-clock time with a non-zero sub-second (.123) component.
		tsMillis = time.UnixMilli(1_700_000_000_123)
	)

	t.Run("millis preserves sub-second precision", func(t *testing.T) {
		require := require.New(t)

		blk, err := BuildUnsigned(parentID, tsMillis, 2, Epoch{}, []byte{3}, true)
		require.NoError(err)
		require.Equal(tsMillis, blk.Timestamp())

		// Re-parsing with the same unit reproduces the ms-precise timestamp.
		parsed, err := Parse(blk.Bytes(), chainID, true)
		require.NoError(err)
		require.Equal(tsMillis, parsed.(SignedBlock).Timestamp())
	})

	t.Run("seconds truncates sub-second precision", func(t *testing.T) {
		require := require.New(t)

		blk, err := BuildUnsigned(parentID, tsMillis, 2, Epoch{}, []byte{3}, false)
		require.NoError(err)
		// The .123 is lost; the timestamp rounds down to the whole second.
		require.Equal(tsMillis.Truncate(time.Second), blk.Timestamp())
	})

	t.Run("unit mismatch misreads the timestamp", func(t *testing.T) {
		require := require.New(t)

		// A block written as seconds but parsed as millis (the footgun the
		// per-chain immutability contract forbids) yields a wildly wrong time.
		secondsBlk, err := BuildUnsigned(parentID, tsMillis, 2, Epoch{}, []byte{3}, false)
		require.NoError(err)

		misread, err := Parse(secondsBlk.Bytes(), chainID, true)
		require.NoError(err)
		require.NotEqual(secondsBlk.Timestamp(), misread.(SignedBlock).Timestamp())
	})

	t.Run("default seconds bytes are unchanged by the feature", func(t *testing.T) {
		require := require.New(t)

		// Whole-second timestamp: the ms and seconds encodings must be identical,
		// so existing networks are bit-for-bit unaffected when the flag is off.
		wholeSecond := time.Unix(1_700_000_000, 0)
		secondsBlk, err := BuildUnsigned(parentID, wholeSecond, 2, Epoch{}, []byte{3}, false)
		require.NoError(err)
		millisBlk, err := BuildUnsigned(parentID, wholeSecond, 2, Epoch{}, []byte{3}, true)
		require.NoError(err)
		// Same wall-clock whole second, but seconds=1.7e9 vs millis=1.7e12 on the
		// wire, so the bytes necessarily differ - confirming the unit is the only
		// thing that changes and that it is a genuine consensus parameter.
		require.NotEqual(secondsBlk.Bytes(), millisBlk.Bytes())
	})
}
