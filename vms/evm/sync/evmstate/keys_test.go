// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithinRange(t *testing.T) {
	t.Parallel()

	end := addPadding(0x00ff, 0xff)

	tests := []struct {
		name string
		key  []byte
		end  []byte
		want bool
	}{
		{
			name: "nil end is unbounded",
			key:  addPadding(0xffff, 0xff),
			want: true,
		},
		{
			name: "empty end is unbounded",
			key:  addPadding(0xffff, 0xff),
			end:  []byte{},
			want: true,
		},
		{
			name: "below end",
			key:  addPadding(0x00fe, 0xff),
			end:  end,
			want: true,
		},
		{
			name: "exactly at end is within",
			key:  end,
			end:  end,
			want: true,
		},
		{
			name: "one byte past end",
			key:  nextRangeKey(end),
			end:  end,
			want: false,
		},
		{
			name: "well past end",
			key:  addPadding(0xff00, 0x00),
			end:  end,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, withinRange(tt.key, tt.end))
		})
	}
}

func TestSegmentRange(t *testing.T) {
	t.Parallel()

	for _, numSegments := range []int{numMainTrieSegments, numStorageTrieSegments, 2} {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			firstStart, _ := segmentRange(0, numSegments)
			require.Equal(t, addPadding(0x0000, 0x00), firstStart, "the first split starts at the bottom")

			_, lastEnd := segmentRange(numSegments-1, numSegments)
			require.Equal(t, addPadding(0xffff, 0xff), lastEnd, "the last split ends at the top")

			for i := 1; i < numSegments; i++ {
				_, prevEnd := segmentRange(i-1, numSegments)
				start, _ := segmentRange(i, numSegments)
				require.Equal(t, start, nextRangeKey(prevEnd), "split %d must start right after %d ends", i, i-1)
			}
		})
	}
}
