// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAllocationCompare(t *testing.T) {
	type test struct {
		name     string
		alloc1   Allocation
		alloc2   Allocation
		expected int
	}
	tests := []test{
		{
			name:     "equal",
			alloc1:   Allocation{},
			alloc2:   Allocation{},
			expected: 0,
		},
		{
			name:   "initial amount smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				InitialAmount: 1,
			},
			expected: -1,
		},
		{
			name:   "bytes smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				AVAXAddr: ids.ShortID{1},
			},
			expected: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.alloc1.Compare(tt.alloc2))
			require.Equal(-tt.expected, tt.alloc2.Compare(tt.alloc1))
		})
	}
}
