// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestBlockBackfillEnabled(t *testing.T) {
	require := require.New(t)
	toEngineCh := make(chan common.Message)
	innerVM, vm, fromInnerVMCh := setupBlockBackfillingVM(t, toEngineCh)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	// 1. Accept a State summary
	var (
		forkHeight                 = uint64(100)
		stateSummaryHeight         = uint64(2023)
		proVMParentStateSummaryBlk = ids.GenerateTestID()
	)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: stateSummaryHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerStateSyncedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.ID{'i', 'n', 'n', 'e', 'r', 'S', 'y', 'n', 'c', 'e', 'd'},
		},
		HeightV: innerSummary.Height(),
		BytesV:  []byte("inner state synced block"),
	}
	stateSummary := createTestStateSummary(t, vm, proVMParentStateSummaryBlk, forkHeight, innerVM, innerSummary, innerStateSyncedBlk)

	// Block backfilling not enabled before state sync is accepted
	ctx := context.Background()
	_, _, err := vm.BackfillBlocksEnabled(ctx)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)

	innerSummary.AcceptF = func(ctx context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}
	_, err = stateSummary.Accept(ctx)
	require.NoError(err)

	// 2. Signal to the ProposerVM that state sync is done
	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerStateSyncedBlk.ID(), nil
	}
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case innerStateSyncedBlk.ID():
			return innerStateSyncedBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	// Block backfilling not enabled before innerVM declares state sync done
	_, _, err = vm.BackfillBlocksEnabled(ctx)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)

	fromInnerVMCh <- common.StateSyncDone
	<-toEngineCh

	// 3. Finally check that block backfilling is enabled looking at innerVM
	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
	}
	_, _, err = vm.BackfillBlocksEnabled(ctx)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)

	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return innerStateSyncedBlk.ID(), innerStateSyncedBlk.Height() - 1, nil
	}
	blkID, _, err := vm.BackfillBlocksEnabled(ctx)
	require.NoError(err)
	require.Equal(proVMParentStateSummaryBlk, blkID)

	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
	}
	_, _, err = vm.BackfillBlocksEnabled(ctx)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)
}

func TestBlockBackfillSuccess(t *testing.T) {
	// setup VM with backfill enabled
	require := require.New(t)
	toEngineCh := make(chan common.Message)
	innerVM, vm, fromInnerVMCh := setupBlockBackfillingVM(t, toEngineCh)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	var (
		forkHeight     = uint64(100)
		blkCount       = 10
		startBlkHeight = uint64(1492)

		// create a list of consecutive blocks and build state summary of top of them
		proBlks, innerBlks = createTestBlocks(t, vm, blkCount, startBlkHeight)

		innerTopBlk        = innerBlks[len(innerBlks)-1]
		proTopBlk          = proBlks[len(proBlks)-1]
		stateSummaryHeight = innerTopBlk.Height() + 1
	)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: stateSummaryHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerStateSyncedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.ID{'i', 'n', 'n', 'e', 'r', 'S', 'y', 'n', 'c', 'e', 'd'},
		},
		ParentV: innerTopBlk.ID(),
		HeightV: innerSummary.Height(),
		BytesV:  []byte("inner state synced block"),
	}
	stateSummary := createTestStateSummary(t, vm, proTopBlk.ID(), forkHeight, innerVM, innerSummary, innerStateSyncedBlk)

	innerSummary.AcceptF = func(ctx context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}

	ctx := context.Background()
	_, err := stateSummary.Accept(ctx)
	require.NoError(err)

	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerStateSyncedBlk.ID(), nil
	}
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case innerStateSyncedBlk.ID():
			return innerStateSyncedBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	fromInnerVMCh <- common.StateSyncDone
	<-toEngineCh

	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return innerStateSyncedBlk.ID(), innerStateSyncedBlk.Height() - 1, nil
	}
	blkID, _, err := vm.BackfillBlocksEnabled(ctx)
	require.NoError(err)
	require.Equal(proTopBlk.ID(), blkID)

	// Backfill some blocks
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, database.ErrNotFound
	}
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if blkID == blk.ID() {
				return blk, nil
			}
		}
		return nil, database.ErrNotFound
	}
	innerVM.BackfillBlocksF = func(_ context.Context, b [][]byte) (ids.ID, uint64, error) {
		lowestblk := innerBlks[0]
		for _, blk := range innerBlks {
			if blk.Height() < lowestblk.Height() {
				lowestblk = blk
			}
		}
		return lowestblk.Parent(), lowestblk.Height() - 1, nil
	}

	blkBytes := make([][]byte, 0, len(proBlks))
	for _, blk := range proBlks {
		blkBytes = append(blkBytes, blk.Bytes())
	}
	nextBlkID, nextBlkHeight, err := vm.BackfillBlocks(ctx, blkBytes)
	require.NoError(err)
	require.Equal(proBlks[0].Parent(), nextBlkID)
	require.Equal(proBlks[0].Height()-1, nextBlkHeight)

	// check proBlocks have been indexed
	for _, blk := range proBlks {
		blkID, err := vm.GetBlockIDAtHeight(ctx, blk.Height())
		require.NoError(err)
		require.Equal(blk.ID(), blkID)

		_, err = vm.GetBlock(ctx, blkID)
		require.NoError(err)
	}
}

func TestBlockBackfillPartialSuccess(t *testing.T) {
	// setup VM with backfill enabled
	require := require.New(t)
	toEngineCh := make(chan common.Message)
	innerVM, vm, fromInnerVMCh := setupBlockBackfillingVM(t, toEngineCh)
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	var (
		forkHeight     = uint64(100)
		blkCount       = 10
		startBlkHeight = uint64(1492)

		// create a list of consecutive blocks and build state summary of top of them
		proBlks, innerBlks = createTestBlocks(t, vm, blkCount, startBlkHeight)

		innerTopBlk        = innerBlks[len(innerBlks)-1]
		proTopBlk          = proBlks[len(proBlks)-1]
		stateSummaryHeight = innerTopBlk.Height() + 1
	)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: stateSummaryHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerStateSyncedBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.ID{'i', 'n', 'n', 'e', 'r', 'S', 'y', 'n', 'c', 'e', 'd'},
		},
		ParentV: innerTopBlk.ID(),
		HeightV: innerSummary.Height(),
		BytesV:  []byte("inner state synced block"),
	}
	stateSummary := createTestStateSummary(t, vm, proTopBlk.ID(), forkHeight, innerVM, innerSummary, innerStateSyncedBlk)

	innerSummary.AcceptF = func(ctx context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}

	ctx := context.Background()
	_, err := stateSummary.Accept(ctx)
	require.NoError(err)

	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerStateSyncedBlk.ID(), nil
	}
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case innerStateSyncedBlk.ID():
			return innerStateSyncedBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	fromInnerVMCh <- common.StateSyncDone
	<-toEngineCh

	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return innerStateSyncedBlk.ID(), innerStateSyncedBlk.Height() - 1, nil
	}
	blkID, height, err := vm.BackfillBlocksEnabled(ctx)
	require.NoError(err)
	require.Equal(proTopBlk.ID(), blkID)
	require.Equal(proTopBlk.Height(), height)

	// Backfill some blocks
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, database.ErrNotFound
	}

	// simulate that lower half of backfilled blocks won't be accepted by innerVM
	idx := len(innerBlks) / 2
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blk := range innerBlks {
			if blkID != blk.ID() {
				continue
			}
			// if it's one of the lower half blocks, assume it's not stored
			// since it was rejected
			if blk.Height() <= innerBlks[idx].Height() {
				return nil, database.ErrNotFound
			}
			return blk, nil
		}
		return nil, database.ErrNotFound
	}

	innerVM.BackfillBlocksF = func(_ context.Context, b [][]byte) (ids.ID, uint64, error) {
		// assume lowest half blocks fails verification
		return innerBlks[idx].ID(), innerBlks[idx].Height(), nil
	}

	blkBytes := make([][]byte, 0, len(proBlks))
	for _, blk := range proBlks {
		blkBytes = append(blkBytes, blk.Bytes())
	}
	nextBlkID, nextBlkHeight, err := vm.BackfillBlocks(ctx, blkBytes)
	require.NoError(err)
	require.Equal(proBlks[idx].ID(), nextBlkID)
	require.Equal(proBlks[idx].Height(), nextBlkHeight)

	// check only upper half of blocks have been indexed
	for i, blk := range proBlks {
		if i <= idx {
			_, err := vm.GetBlockIDAtHeight(ctx, blk.Height())
			require.ErrorIs(err, database.ErrNotFound)

			_, err = vm.GetBlock(ctx, blk.ID())
			require.ErrorIs(err, database.ErrNotFound)
		} else {
			blkID, err := vm.GetBlockIDAtHeight(ctx, blk.Height())
			require.NoError(err)
			require.Equal(blk.ID(), blkID)

			_, err = vm.GetBlock(ctx, blkID)
			require.NoError(err)
		}
	}
}

func createTestBlocks(
	t *testing.T,
	vm *VM,
	blkCount int,
	startBlkHeight uint64,
) (
	[]snowman.Block, // proposerVM blocks
	[]snowman.Block, // inner VM blocks
) {
	require := require.New(t)
	var (
		latestInnerBlkID = ids.GenerateTestID()
		latestProBlkID   = ids.GenerateTestID()

		dummyBlkTime      = time.Now()
		dummyPChainHeight = uint64(1492)

		innerBlks = make([]snowman.Block, 0, blkCount)
		proBlks   = make([]snowman.Block, 0, blkCount)
	)
	for idx := 0; idx < blkCount; idx++ {
		rndBytes := ids.GenerateTestID()
		innerBlkTop := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:  rndBytes[:],
			ParentV: latestInnerBlkID,
			HeightV: startBlkHeight + uint64(idx),
		}
		latestInnerBlkID = innerBlkTop.ID()
		innerBlks = append(innerBlks, innerBlkTop)

		statelessChild, err := statelessblock.BuildUnsigned(
			latestProBlkID,
			dummyBlkTime,
			dummyPChainHeight,
			innerBlkTop.Bytes(),
		)
		require.NoError(err)
		proBlkTop := &postForkBlock{
			SignedBlock: statelessChild,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlkTop,
				status:   choices.Processing,
			},
		}
		latestProBlkID = proBlkTop.ID()
		proBlks = append(proBlks, proBlkTop)
	}
	return proBlks, innerBlks
}

func createTestStateSummary(
	t *testing.T,
	vm *VM,
	proVMParentStateSummaryBlk ids.ID,
	forkHeight uint64,
	innerVM *fullVM,
	innerSummary *block.TestStateSummary,
	innerBlk *snowman.TestBlock,
) block.StateSummary {
	require := require.New(t)
	slb, err := statelessblock.Build(
		proVMParentStateSummaryBlk,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		vm.stakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.stakingLeafSigner,
	)
	require.NoError(err)

	statelessSummary, err := summary.Build(forkHeight, slb.Bytes(), innerSummary.Bytes())
	require.NoError(err)

	innerVM.ParseStateSummaryF = func(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
		require.Equal(innerSummary.BytesV, summaryBytes)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	summary, err := vm.ParseStateSummary(context.Background(), statelessSummary.Bytes())
	require.NoError(err)
	return summary
}

func setupBlockBackfillingVM(
	t *testing.T,
	toEngineCh chan<- common.Message,
) (
	*fullVM,
	*VM,
	chan<- common.Message,
) {
	require := require.New(t)

	innerVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
		TestStateSyncableVM: &block.TestStateSyncableVM{
			T: t,
		},
	}

	// signal height index is complete
	innerVM.VerifyHeightIndexF = func(context.Context) error {
		return nil
	}

	// load innerVM expectations
	innerGenesisBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.ID{'i', 'n', 'n', 'e', 'r', 'G', 'e', 'n', 'e', 's', 'i', 's', 'I', 'D'},
		},
		HeightV: 0,
		BytesV:  []byte("genesis state"),
	}

	toProVMChannel := make(chan<- common.Message)
	innerVM.InitializeF = func(_ context.Context, _ *snow.Context, _ manager.Manager,
		_ []byte, _ []byte, _ []byte, ch chan<- common.Message,
		_ []*common.Fx, _ common.AppSender,
	) error {
		toProVMChannel = ch
		return nil
	}
	innerVM.VerifyHeightIndexF = func(context.Context) error {
		return nil
	}
	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerGenesisBlk.ID(), nil
	}
	innerVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		return innerGenesisBlk, nil
	}

	// createVM
	dbManager := manager.NewMemDB(version.Semantic1_0_0)
	dbManager = dbManager.NewPrefixDBManager([]byte{})

	vm := New(
		innerVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		DefaultNumHistoricalBlocks,
		pTestSigner,
		pTestCert,
	)

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		innerGenesisBlk.Bytes(),
		nil,
		nil,
		toEngineCh,
		nil,
		nil,
	))

	return innerVM, vm, toProVMChannel
}
