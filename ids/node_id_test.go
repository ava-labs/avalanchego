// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeIDShortNodeIDConversion(t *testing.T) {
	require := require.New(t)

	nonEmptyInputs := []ShortNodeID{
		{24},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}

	for _, input := range nonEmptyInputs {
		nodeID := NodeIDFromShortNodeID(input)
		require.Equal(nodeID.String(), input.String())
		require.Equal(nodeID.Bytes(), input.Bytes())

		output, err := ShortNodeIDFromNodeID(nodeID)
		require.NoError(err)
		require.Equal(input, output)
	}

	// Empty ids work differently
	require.Equal(EmptyNodeID, NodeIDFromShortNodeID(EmptyShortNodeID))
	require.NotEqual(EmptyNodeID.String(), EmptyShortNodeID.String())
	require.NotEqual(EmptyNodeID.Bytes(), EmptyShortNodeID.Bytes())
}
