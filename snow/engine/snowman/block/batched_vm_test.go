// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var errTest = errors.New("non-nil error")

func TestGetAncestorsDatabaseNotFound(t *testing.T) {
	vm := &TestVM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(t, someID, id)
		return nil, database.ErrNotFound
	}
	containers, err := GetAncestors(context.Background(), vm, someID, 10, 10, 1*time.Second)
	require.NoError(t, err)
	require.Len(t, containers, 0)
}

// TestGetAncestorsPropagatesErrors checks errors other than
// database.ErrNotFound propagate to caller.
func TestGetAncestorsPropagatesErrors(t *testing.T) {
	vm := &TestVM{}
	someID := ids.GenerateTestID()
	vm.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(t, someID, id)
		return nil, errTest
	}
	containers, err := GetAncestors(context.Background(), vm, someID, 10, 10, 1*time.Second)
	require.Nil(t, containers)
	require.ErrorIs(t, err, errTest)
}
