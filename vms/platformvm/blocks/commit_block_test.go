// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestNewBlueberryCommitBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewBlueberryCommitBlock(
		time.Now(),
		ids.GenerateTestID(),
		1337,
	)
	require.NoError(err)

	// Make sure the block is initialized
	require.NotNil(blk.Bytes())
}

func TestNewApricotCommitBlock(t *testing.T) {
	require := require.New(t)

	blk, err := NewApricotCommitBlock(
		ids.GenerateTestID(),
		1337,
	)
	require.NoError(err)

	// Make sure the block is initialized
	require.NotNil(blk.Bytes())
}
