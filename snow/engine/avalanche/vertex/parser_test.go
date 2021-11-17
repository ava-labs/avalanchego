// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestParseInvalid(t *testing.T) {
	vtxBytes := []byte{}
	_, err := Parse(vtxBytes)
	assert.Error(t, err, "parse on an invalid vertex should have errored")
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
	assert.NoError(t, err)

	vtxBytes := vtx.Bytes()
	parsedVtx, err := Parse(vtxBytes)
	assert.NoError(t, err)
	assert.Equal(t, vtx, parsedVtx)
}
