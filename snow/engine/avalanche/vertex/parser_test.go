// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestParseInvalid(t *testing.T) {
	vtxBytes := []byte{}
	_, err := Parse(vtxBytes)
	require.Error(t, err, "parse on an invalid vertex should have errored")
}

func TestParseValid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{6}, {7}}
	vtx, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	require.NoError(t, err)

	vtxBytes := vtx.Bytes()
	parsedVtx, err := Parse(vtxBytes)
	require.NoError(t, err)
	require.Equal(t, vtx, parsedVtx)
}
