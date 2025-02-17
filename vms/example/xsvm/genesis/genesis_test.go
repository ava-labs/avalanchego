// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestGenesis(t *testing.T) {
	require := require.New(t)

	id, err := ids.ShortFromString("6Y3kysjF9jnHnYkdS9yGAuoHyae2eNmeV")
	require.NoError(err)
	id2, err := ids.ShortFromString("LeKrndtsMxcLMzHz3w4uo1XtLDpfi66c")
	require.NoError(err)

	genesis := Genesis{
		Timestamp: 123,
		Allocations: []Allocation{
			{Address: id, Balance: 1000000000},
			{Address: id2, Balance: 3000000000},
		},
	}
	bytes, err := Codec.Marshal(CodecVersion, genesis)
	require.NoError(err)

	parsed, err := Parse(bytes)
	require.NoError(err)
	require.Equal(genesis, *parsed)
}
