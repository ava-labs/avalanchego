// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/database"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow/consensus/snowman"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block/blocktest"
	"github.com/MetalBlockchain/metalgo/utils/logging"

	. "github.com/MetalBlockchain/metalgo/snow/engine/snowman/block"
)

var errTest = errors.New("non-nil error")

func TestGetAncestorsDatabaseNotFound(t *testing.T) {
	require := require.New(t)

	vm := &blocktest.VM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(someID, id)
		return nil, database.ErrNotFound
	}
	containers, err := GetAncestors(t.Context(), logging.NoLog{}, vm, someID, 10, 10, 1*time.Second)
	require.NoError(err)
	require.Empty(containers)
}

// TestGetAncestorsPropagatesErrors checks errors other than
// database.ErrNotFound propagate to caller.
func TestGetAncestorsPropagatesErrors(t *testing.T) {
	require := require.New(t)

	vm := &blocktest.VM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(someID, id)
		return nil, errTest
	}
	containers, err := GetAncestors(t.Context(), logging.NoLog{}, vm, someID, 10, 10, 1*time.Second)
	require.Nil(containers)
	require.ErrorIs(err, errTest)
}
