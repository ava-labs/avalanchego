// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestSample(t *testing.T) {
	tests := []struct {
		name            string
		elements        map[ids.NodeID]uint64
		maxSize         int
		expectedSampled set.Set[ids.NodeID]
		expectedErr     error
	}{
		{
			name: "sample everything",
			elements: map[ids.NodeID]uint64{
				nodeID0: 1,
				nodeID1: 1,
			},
			maxSize:         2,
			expectedSampled: set.Of(nodeID0, nodeID1),
			expectedErr:     nil,
		},
		{
			name: "limit sample due to too few elements",
			elements: map[ids.NodeID]uint64{
				nodeID0: 1,
			},
			maxSize:         2,
			expectedSampled: set.Of(nodeID0),
			expectedErr:     nil,
		},
		{
			name: "limit sample",
			elements: map[ids.NodeID]uint64{
				nodeID0: math.MaxUint64 - 1,
				nodeID1: 1,
			},
			maxSize:         1,
			expectedSampled: set.Of(nodeID0),
			expectedErr:     nil,
		},
		{
			name: "overflow",
			elements: map[ids.NodeID]uint64{
				nodeID0: math.MaxUint64,
				nodeID1: 1,
			},
			maxSize:         1,
			expectedSampled: nil,
			expectedErr:     safemath.ErrOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			sampled, err := Sample(test.elements, test.maxSize)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedSampled, sampled)
		})
	}
}
