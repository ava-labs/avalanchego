// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightedArrayElementLess(t *testing.T) {
	require := require.New(t)

	var elt1, elt2 weightedArrayElement
	require.False(elt1.Less(elt2))
	require.False(elt2.Less(elt1))

	elt1 = weightedArrayElement{
		cumulativeWeight: 1,
	}
	elt2 = weightedArrayElement{
		cumulativeWeight: 2,
	}
	require.False(elt1.Less(elt2))
	require.True(elt2.Less(elt1))
}
