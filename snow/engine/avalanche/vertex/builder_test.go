// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBuildInvalid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{6}, {6}}
	_, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	assert.Error(t, err, "build should have errored because restrictions were provided in epoch 0")
}

func TestBuildValid(t *testing.T) {
	chainID := ids.ID{1}
	height := uint64(2)
	parentIDs := []ids.ID{{4}, {5}}
	txs := [][]byte{{7}, {6}}
	vtx, err := Build(
		chainID,
		height,
		parentIDs,
		txs,
	)
	assert.NoError(t, err)
	assert.Equal(t, chainID, vtx.ChainID())
	assert.Equal(t, height, vtx.Height())
	assert.Equal(t, parentIDs, vtx.ParentIDs())
	assert.Equal(t, txs, vtx.Txs())
}
