// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gasprice

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestFeeInfoProvider(t *testing.T) {
	backend := newTestBackend(t, params.TestChainConfig, 2, common.Big0, testGenBlock(t, 55, 370))
	f, err := newFeeInfoProvider(backend, 1, 2)
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
	backend := newTestBackend(t, params.TestChainConfig, 0, common.Big0, testGenBlock(t, 55, 370))
	f, err := newFeeInfoProvider(backend, 1, size)
	require.NoError(t, err)

	// add [overflow] more elements than what will fit in the cache
	// to test eviction behavior.
	for i := 0; i < size+feeCacheExtraSlots+overflow; i++ {
		header := &types.Header{Number: big.NewInt(int64(i))}
		_, err := f.addHeader(context.Background(), header)
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
