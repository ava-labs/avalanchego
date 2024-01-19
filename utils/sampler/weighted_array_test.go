// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightedArrayElementCompare(t *testing.T) {
	tests := []struct {
		a        weightedArrayElement
		b        weightedArrayElement
		expected int
	}{
		{
			a:        weightedArrayElement{},
			b:        weightedArrayElement{},
			expected: 0,
		},
		{
			a: weightedArrayElement{
				cumulativeWeight: 1,
			},
			b: weightedArrayElement{
				cumulativeWeight: 2,
			},
			expected: 1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%d", test.a.cumulativeWeight, test.b.cumulativeWeight, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}
