// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
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
	stateSummaryHeight := uint64(2023)
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
	stateSummary, stateSummaryParentBlkID := createTestStateSummary(t, innerVM, vm, innerSummary, innerStateSyncedBlk)

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

	// Block backfilling not enabled until innerVM says so
	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
	}
	_, _, err = vm.BackfillBlocksEnabled(ctx)
	require.ErrorIs(err, block.ErrBlockBackfillingNotEnabled)

	// 3. Finally check that block backfilling is done
	innerVM.BackfillBlocksEnabledF = func(ctx context.Context) (ids.ID, uint64, error) {
		return innerStateSyncedBlk.ID(), innerStateSyncedBlk.Height() - 1, nil
	}
	blkID, _, err := vm.BackfillBlocksEnabled(ctx)
	require.NoError(err)
	require.Equal(stateSummaryParentBlkID, blkID)
}

func createTestStateSummary(
	t *testing.T,
	innerVM *fullVM,
	vm *VM,
	innerSummary *block.TestStateSummary,
	innerBlk *snowman.TestBlock,
) (block.StateSummary, ids.ID) {
	require := require.New(t)
	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		vm.stakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.stakingLeafSigner,
	)
	require.NoError(err)

	statelessSummary, err := summary.Build(innerSummary.Height()-1, slb.Bytes(), innerSummary.Bytes())
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
	return summary, slb.ParentID()
}

func setupBlockBackfillingVM(t *testing.T, toEngineCh chan<- common.Message) (*fullVM, *VM, chan<- common.Message) {
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
