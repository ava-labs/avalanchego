// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gasprice

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core"
)

func TestFeeInfoProvider(t *testing.T) {
	backend := newTestBackend(t, 2, testGenBlock(t, 55, 80))
	f, err := newFeeInfoProvider(backend, 2)
	require.NoError(t, err)

	// check that accepted event was subscribed
	require.NotNil(t, backend.acceptedEvent)

	// check fee infos were cached
	require.Equal(t, 2, f.cache.Len())

	// some block that extends the current chain
	var wg sync.WaitGroup
	wg.Add(1)
	f.newHeaderAdded = func() { wg.Done() }
	header := &types.Header{Number: big.NewInt(3), ParentHash: backend.LastAcceptedBlock().Hash()}
	block := types.NewBlockWithHeader(header)
	backend.acceptedEvent <- core.ChainEvent{Block: block}

	// wait for the event to process before validating the new header was added.
	wg.Wait()
	feeInfo, ok := f.get(3)
	require.True(t, ok)
	require.NotNil(t, feeInfo)
}

func TestFeeInfoProviderCacheSize(t *testing.T) {
	size := 5
	overflow := 3
	backend := newTestBackend(t, 0, testGenBlock(t, 55, 370))
	f, err := newFeeInfoProvider(backend, size)
	require.NoError(t, err)

	// add [overflow] more elements than what will fit in the cache
	// to test eviction behavior.
	for i := 0; i < size+feeCacheExtraSlots+overflow; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		_, err := f.addHeader(context.Background(), header, []*types.Transaction{})
		require.NoError(t, err)
	}

	// these numbers should be evicted
	for i := 0; i < overflow; i++ {
		feeInfo, ok := f.get(uint64(i))
		require.False(t, ok)
		require.Nil(t, feeInfo)
	}

	// these numbers should be present
	for i := overflow; i < size+feeCacheExtraSlots+overflow; i++ {
		feeInfo, ok := f.get(uint64(i))
		require.True(t, ok)
		require.NotNil(t, feeInfo)
	}
}
