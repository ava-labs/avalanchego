// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestBlockBuilder(t *testing.T) {
	ctx := t.Context()
	genesis := newTestBlock(t, newBlockConfig{})
	child := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})

	tests := []struct {
		name          string
		block         snowman.Block
		shouldBuild   bool
		expectedBlock simplex.VerifiedBlock
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
			require.Equal(t, tt.expectedBlock, block)
			if tt.expectedBlock == nil {
				require.Greater(t, count, 1)
			}
		})
	}
}

func TestBlockBuilderCancelContext(t *testing.T) {
	ctx := t.Context()
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

func TestWaitForPendingBlock(t *testing.T) {
	ctx := t.Context()
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

	bb.WaitForPendingBlock(ctx)
	require.Equal(t, 1, count)
}

func TestBlockBuildingExponentialBackoff(t *testing.T) {
	ctx := t.Context()
	vm := newTestVM()
	genesis := newTestBlock(t, newBlockConfig{})
	child := newTestBlock(t, newBlockConfig{
		prev: genesis,
	})
	const (
		failedAttempts       = 7
		minimumExpectedDelay = initBackoff * (1<<failedAttempts - 1)
	)

	vm.WaitForEventF = func(_ context.Context) (common.Message, error) {
		return common.PendingTxs, nil
	}

	count := 0
	vm.BuildBlockF = func(_ context.Context) (snowman.Block, error) {
		if count < failedAttempts {
			count++
			return nil, errors.New("failed to build block")
		}

		// on the 8th try, return the block successfully
		return child.vmBlock, nil
	}

	bb := &BlockBuilder{
		log:          logging.NoLog{},
		vm:           vm,
		blockTracker: genesis.blockTracker,
	}

	start := time.Now()
	block, built := bb.BuildBlock(ctx, child.BlockHeader().ProtocolMetadata)
	endTime := time.Since(start)

	require.True(t, built)
	require.Equal(t, child.BlockHeader(), block.BlockHeader())

	// 10, 20, 40, 80, 160, 320, 640 = 1270ms
	require.GreaterOrEqual(t, endTime, minimumExpectedDelay)
}

func TestWaitForPendingBlockBackoff(t *testing.T) {
	ctx := t.Context()
	vm := newTestVM()
	const (
		failedAttempts       = 7
		minimumExpectedDelay = initBackoff * (1<<failedAttempts - 1)
	)

	count := 0
	vm.WaitForEventF = func(_ context.Context) (common.Message, error) {
		if count < failedAttempts {
			count++
			return common.StateSyncDone, nil
		}

		return common.PendingTxs, nil
	}

	bb := &BlockBuilder{
		log:          logging.NoLog{},
		vm:           vm,
		blockTracker: nil,
	}

	start := time.Now()
	bb.WaitForPendingBlock(ctx)
	endTime := time.Since(start)

	// 10, 20, 40, 80, 160, 320, 640 = 1270ms
	require.GreaterOrEqual(t, endTime, minimumExpectedDelay)
}
