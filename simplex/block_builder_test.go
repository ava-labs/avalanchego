// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestBlockBuilder(t *testing.T) {
	ctx := context.Background()
	genesis := newTestBlock(t, newBlockConfig{})
	child := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	tests := []struct {
		name          string
		block         snowman.Block
		shouldBuild   bool
		expectedBlock *Block
		vmBlockBuildF func(ctx context.Context) (snowman.Block, error)
	}{
		{
			name:          "build block successfully",
			block:         child.vmBlock,
			shouldBuild:   true,
			expectedBlock: child,
			vmBlockBuildF: func(_ context.Context) (snowman.Block, error) {
				return child.vmBlock, nil
			},
		},
		{
			name:        "fail to build block",
			block:       nil,
			shouldBuild: false,
			vmBlockBuildF: func(_ context.Context) (snowman.Block, error) {
				return nil, errors.New("failed to build block")
			},
		},
		{
			name:  "fail to verify block",
			block: nil,
			vmBlockBuildF: func(_ context.Context) (snowman.Block, error) {
				b := newTestBlock(t, newBlockConfig{
					prev: genesis,
				})
				b.vmBlock.(*wrappedBlock).VerifyV = errors.New("verification failed")
				return b.vmBlock, nil
			},
			shouldBuild: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := 0
			vm := newTestVM()

			vm.WaitForEventF = func(_ context.Context) (common.Message, error) {
				count++
				return common.PendingTxs, nil
			}
			vm.BuildBlockF = tt.vmBlockBuildF

			bb := &BlockBuilder{
				log:          logging.NoLog{},
				vm:           vm,
				blockTracker: genesis.blockTracker,
			}

			timeoutCtx, cancelCtx := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancelCtx()

			block, built := bb.BuildBlock(timeoutCtx, child.BlockHeader().ProtocolMetadata)
			require.Equal(t, tt.shouldBuild, built)
			if tt.expectedBlock == nil {
				require.Nil(t, block)
				require.Greater(t, count, 1)
			} else {
				require.Equal(t, tt.expectedBlock, block)
			}
		})
	}
}

func TestBlockBuilderCancelContext(t *testing.T) {
	ctx := context.Background()
	vm := newTestVM()
	genesis := newTestBlock(t, newBlockConfig{})
	child := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})
	vm.WaitForEventF = func(ctx context.Context) (common.Message, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}

	bb := &BlockBuilder{
		log:          logging.NoLog{},
		vm:           vm,
		blockTracker: genesis.blockTracker,
	}

	timeoutCtx, cancelCtx := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelCtx()

	_, built := bb.BuildBlock(timeoutCtx, child.BlockHeader().ProtocolMetadata)
	require.False(t, built, "Block should not be built when context is cancelled")
}

func TestIncomingBlock(t *testing.T) {
	ctx := context.Background()
	vm := newTestVM()
	genesis := newTestBlock(t, newBlockConfig{})
	count := 0
	vm.WaitForEventF = func(_ context.Context) (common.Message, error) {
		if count == 0 {
			count++
			return common.StateSyncDone, nil
		}
		return common.PendingTxs, nil
	}

	bb := &BlockBuilder{
		log:          logging.NoLog{},
		vm:           vm,
		blockTracker: genesis.blockTracker,
	}

	bb.IncomingBlock(ctx)
	require.Equal(t, 1, count)
}

func TestBlockBuildingExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	vm := newTestVM()
	genesis := newTestBlock(t, newBlockConfig{})
	child := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	count := 0
	vm.WaitForEventF = func(_ context.Context) (common.Message, error) {
		count++
		return common.PendingTxs, nil
	}

	vm.BuildBlockF = func(_ context.Context) (snowman.Block, error) {
		if count > 7 {
			return child.vmBlock, nil
		}
		return nil, errors.New("failed to build block")
	}

	bb := &BlockBuilder{
		log:          logging.NoLog{},
		vm:           vm,
		blockTracker: genesis.blockTracker,
	}

	timeoutTime := 10 * time.Second
	timeoutCtx, cancelCtx := context.WithTimeout(ctx, timeoutTime)
	defer cancelCtx()

	start := time.Now()
	block, built := bb.BuildBlock(timeoutCtx, child.BlockHeader().ProtocolMetadata)
	endTime := time.Since(start)

	require.True(t, built)
	require.Equal(t, child.BlockHeader(), block.BlockHeader())

	// 10, 20, 40, 80, 160, 320, 640 = 1270ms
	require.GreaterOrEqual(t, endTime.Milliseconds(), int64(1270))
	// ensure we haven't timed out
	require.NotEqual(t, timeoutTime, endTime)
}
