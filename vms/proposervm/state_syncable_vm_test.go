// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func helperBuildStateSyncTestObjects(t *testing.T) (*fullVM, *VM) {
	require := require.New(t)

	innerVM := &fullVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{
				T: t,
			},
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
		},
	}

	// load innerVM expectations
	innerVM.InitializeF = func(context.Context, *snow.Context, database.Database,
		[]byte, []byte, []byte,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	innerVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID != snowmantest.Genesis.ID() {
			return nil, database.ErrNotFound
		}
		return snowmantest.Genesis, nil
	}

	// create the VM
	vm := New(
		innerVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()),
		snowmantest.GenesisBytes,
		nil,
		nil,
		nil,
		nil,
	))
	require.NoError(vm.SetState(t.Context(), snow.StateSyncing))

	return innerVM, vm
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	// ProposerVM State Sync disabled if innerVM State sync is disabled
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return false, nil
	}
	enabled, err := vm.StateSyncEnabled(t.Context())
	require.NoError(err)
	require.False(enabled)

	// ProposerVM State Sync enabled if innerVM State sync is enabled
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return true, nil
	}
	enabled, err = vm.StateSyncEnabled(t.Context())
	require.NoError(err)
	require.True(enabled)
}

func TestStateSyncGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No ongoing state summary case
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetOngoingSyncStateSummary(t.Context())
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vm.GetOngoingSyncStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetOngoingSyncStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk))

	summary, err = vm.GetOngoingSyncStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestStateSyncGetLastStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No last state summary case
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetLastStateSummary(t.Context())
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vm.GetLastStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetLastStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk))

	summary, err = vm.GetLastStateSummary(t.Context())
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestStateSyncGetStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No state summary case
	innerVM.GetStateSummaryF = func(context.Context, uint64) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetStateSummary(t.Context(), reqHeight)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk))

	summary, err = vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		require.Equal(summaryBytes, innerSummary.Bytes())
		return innerSummary, nil
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}

	// Get a pre fork block than parse it
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err := vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)

	parsedSummary, err := vm.ParseStateSummary(t.Context(), summary.Bytes())
	require.NoError(err)
	require.Equal(summary.ID(), parsedSummary.ID())
	require.Equal(summary.Height(), parsedSummary.Height())
	require.Equal(summary.Bytes(), parsedSummary.Bytes())

	// Get a post fork block than parse it
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk))
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))
	summary, err = vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)

	parsedSummary, err = vm.ParseStateSummary(t.Context(), summary.Bytes())
	require.NoError(err)
	require.Equal(summary.ID(), parsedSummary.ID())
	require.Equal(summary.Height(), parsedSummary.Height())
	require.Equal(summary.Bytes(), parsedSummary.Bytes())
}

func TestStateSummaryAccept(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)

	statelessSummary, err := summary.Build(innerSummary.Height()-1, slb.Bytes(), innerSummary.Bytes())
	require.NoError(err)

	innerVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		require.Equal(innerSummary.BytesV, summaryBytes)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	summary, err := vm.ParseStateSummary(t.Context(), statelessSummary.Bytes())
	require.NoError(err)

	// test Accept accepted
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}
	status, err := summary.Accept(t.Context())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

	// test Accept skipped
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		return block.StateSyncSkipped, nil
	}
	status, err = summary.Accept(t.Context())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)
}

func TestStateSummaryAcceptOlderBlock(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// Set the last accepted block height to be higher than the state summary
	// we are going to attempt to accept
	vm.lastAcceptedHeight = innerSummary.Height() + 1

	// store post fork block associated with summary
	innerBlk := &snowmantest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vm.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk))

	summary, err := vm.GetStateSummary(t.Context(), reqHeight)
	require.NoError(err)
	require.Equal(summary.Height(), reqHeight)

	// test Accept summary invokes innerVM
	calledInnerAccept := false
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
			return innerSummary.ID(), nil
		}
		innerVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
			return innerBlk, nil
		}
		calledInnerAccept = true
		return block.StateSyncStatic, nil
	}
	status, err := summary.Accept(t.Context())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)
	require.True(calledInnerAccept)

	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))
	require.Equal(summary.Height(), vm.lastAcceptedHeight)
	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(proBlk.ID(), lastAcceptedID)
}

// TestStateSummaryAcceptOlderBlockSkipStateSync tests the case where we accept
// a state summary older than the last accepted block. In this case, we should not
// roll the ProposerVM back to match the state summary, but we should invoke the
// innerVM to accept the state summary and re-align the ProposerVM with the innerVM
// during the transition out of state sync.
func TestStateSummaryAcceptOlderBlockSkipStateSync(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	// store post fork block associated with summary
	innerBlk1 := &snowmantest.Block{
		Decidable: snowtest.Decidable{
			IDV: ids.GenerateTestID(),
		},
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: 1969,
	}
	innerBlk2 := snowmantest.BuildChild(innerBlk1)

	innerSummary1 := &blocktest.StateSummary{
		IDV:     innerBlk1.ID(),
		HeightV: innerBlk1.Height(),
		BytesV:  innerBlk1.BytesV,
	}

	require.NoError(vm.SetForkHeight(innerSummary1.Height() - 1))

	// Set the last accepted block height to be higher than the state summary
	// we are going to attempt to accept
	vm.lastAcceptedHeight = innerBlk2.Height()

	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerBlk2.IDV, nil
	}

	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case innerBlk1.ID():
			return innerBlk1, nil
		case innerBlk2.ID():
			return innerBlk2, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	innerVM.GetStateSummaryF = func(context.Context, uint64) (block.StateSummary, error) {
		return innerSummary1, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, innerBlk1.BytesV):
			return innerBlk1, nil
		case bytes.Equal(b, innerBlk2.BytesV):
			return innerBlk2, nil
		default:
			require.FailNow("unexpected parse block")
			// Unreachable, but required to satisfy the compiler
			// since we use FailNow instead of panic
			return nil, nil
		}
	}
	calledInnerAccept := false
	innerSummary1.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		calledInnerAccept = true
		return block.StateSyncSkipped, nil
	}

	slb1, err := statelessblock.Build(
		vm.preferred,
		innerBlk1.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk1.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk1 := &postForkBlock{
		SignedBlock: slb1,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk1,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk1))

	slb2, err := statelessblock.Build(
		vm.preferred,
		innerBlk2.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{},
		vm.StakingCertLeaf,
		innerBlk2.Bytes(),
		vm.ctx.ChainID,
		vm.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk2 := &postForkBlock{
		SignedBlock: slb2,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk2,
		},
	}
	require.NoError(vm.acceptPostForkBlock(proBlk2))

	summary, err := vm.GetStateSummary(t.Context(), innerBlk1.Height())
	require.NoError(err)
	require.Equal(innerBlk1.Height(), summary.Height())

	// Process a state summary that would rewind the chain
	// ProposerVM should ignore the rollback and accept the inner state summary to
	// notify the innerVM.
	// This can result in the ProposerVM and innerVM diverging their last accepted block.
	// These are re-aligned in SetState before transitioning to consensus.
	status, err := summary.Accept(t.Context())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)
	require.True(calledInnerAccept)
	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))

	require.Equal(innerBlk2.Height(), vm.lastAcceptedHeight)
	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(proBlk2.ID(), lastAcceptedID)
}
