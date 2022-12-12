// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAllocationLess(t *testing.T) {
	type test struct {
		name     string
		alloc1   Allocation
		alloc2   Allocation
		expected bool
	}
	tests := []test{
		{
			name:     "equal",
			alloc1:   Allocation{},
			alloc2:   Allocation{},
			expected: false,
		},
		{
			name:   "first initial amount smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				InitialAmount: 1,
			},
			expected: true,
		},
		{
			name: "first initial amount larger",
			alloc1: Allocation{
				InitialAmount: 1,
			},
			alloc2:   Allocation{},
			expected: false,
		},
		{
			name:   "first bytes smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				AVAXAddr: ids.ShortID{1},
			},
			expected: true,
		},
		{
			name: "first bytes larger",
			alloc1: Allocation{
				AVAXAddr: ids.ShortID{1},
			},
			alloc2:   Allocation{},
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.expected, tt.alloc1.Less(tt.alloc2))
		})
	}
}
