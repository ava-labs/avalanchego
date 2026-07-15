// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TestMillisecondTimestampRoundTrip proves that with millisecondTimestamps=true
// the wrapper block's int64 timestamp carries sub-second precision through
// build -> serialize -> parse, while the default (seconds) truncates it.
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

		parsed, err := Parse(blk.Bytes(), chainID, true)
		require.NoError(err)
		require.Equal(tsMillis, parsed.(SignedBlock).Timestamp())
	})

	t.Run("seconds truncates sub-second precision", func(t *testing.T) {
		require := require.New(t)

		blk, err := BuildUnsigned(parentID, tsMillis, 2, Epoch{}, []byte{3}, false)
		require.NoError(err)
		require.Equal(tsMillis.Truncate(time.Second), blk.Timestamp())
	})

	t.Run("seconds encoding is byte-stable", func(t *testing.T) {
		require := require.New(t)

		// Golden bytes pin the seconds encoding so it can never silently change.
		const goldenHex = "0000000000000100000000000000000000000000000000000000000000000000000000000000000000006553f100000000000000000200000000000000010300000000"
		blk, err := BuildUnsigned(parentID, time.Unix(1_700_000_000, 0), 2, Epoch{}, []byte{3}, false)
		require.NoError(err)
		require.Equal(goldenHex, hex.EncodeToString(blk.Bytes()))
	})
}
