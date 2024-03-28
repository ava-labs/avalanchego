// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalDimensions(t *testing.T) {
	require := require.New(t)

	input := Dimensions{0, 1, 2024, math.MaxUint64}
	bytes := input.Bytes()
	var output Dimensions
	require.NoError(output.FromBytes(bytes))
	require.Equal(input, output)
}
