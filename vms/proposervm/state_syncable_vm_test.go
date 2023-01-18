// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"errors"
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

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var errUnknownSummary = errors.New("unknown summary")

func helperBuildStateSyncTestObjects(t *testing.T) (*fullVM, *VM) {
	innerVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
		TestHeightIndexedVM: &block.TestHeightIndexedVM{
			T: t,
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
			IDV: ids.ID{'i', 'n', 'n', 'e', 'r', 'G', 'e', 'n', 's', 'y', 's', 'I', 'D'},
		},
		HeightV: 0,
		BytesV:  []byte("genesis state"),
	}
	innerVM.InitializeF = func(context.Context, *snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
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
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)

	err := vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		innerGenesisBlk.Bytes(),
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	return innerVM, vm
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)

	// ProposerVM State Sync disabled if innerVM State sync is disabled
	vm.hIndexer.MarkRepaired(true)
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return false, nil
	}
	enabled, err := vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// ProposerVM State Sync enabled if innerVM State sync is enabled
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return true, nil
	}
	enabled, err = vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)
}

func TestStateSyncGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No ongoing state summary case
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetOngoingSyncStateSummary(context.Background())
	require.True(err == database.ErrNotFound)
	require.True(summary == nil)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.Equal(err, database.ErrNotFound)
	summary, err = vm.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Pre fork summary case, fork height already reached
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Post fork summary case
	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))

	summary, err = vm.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.Height() == innerSummary.Height())
}

func TestStateSyncGetLastStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No last state summary case
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetLastStateSummary(context.Background())
	require.True(err == database.ErrNotFound)
	require.True(summary == nil)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.Equal(err, database.ErrNotFound)
	summary, err = vm.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Pre fork summary case, fork height already reached
	innerVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Post fork summary case
	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))

	summary, err = vm.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.Height() == innerSummary.Height())
}

func TestStateSyncGetStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	reqHeight := uint64(1969)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No state summary case
	innerVM.GetStateSummaryF = func(context.Context, uint64) (block.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vm.GetStateSummary(context.Background(), reqHeight)
	require.True(err == database.ErrNotFound)
	require.True(summary == nil)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.True(h == reqHeight)
		return innerSummary, nil
	}
	_, err = vm.GetForkHeight()
	require.Equal(err, database.ErrNotFound)
	summary, err = vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Pre fork summary case, fork height already reached
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.True(h == reqHeight)
		return innerSummary, nil
	}
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.True(summary.ID() == innerSummary.ID())
	require.True(summary.Height() == innerSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), innerSummary.Bytes()))

	// Post fork summary case
	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))

	summary, err = vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.True(summary.Height() == innerSummary.Height())
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	innerVM, vm := helperBuildStateSyncTestObjects(t)
	reqHeight := uint64(1969)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (block.StateSummary, error) {
		require.Equal(summaryBytes, innerSummary.Bytes())
		return innerSummary, nil
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.True(h == reqHeight)
		return innerSummary, nil
	}

	// Get a pre fork block than parse it
	require.NoError(vm.SetForkHeight(innerSummary.Height() + 1))
	summary, err := vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	parsedSummary, err := vm.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)
	require.True(summary.ID() == parsedSummary.ID())
	require.True(summary.Height() == parsedSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), parsedSummary.Bytes()))

	// Get a post fork block than parse it
	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))
	summary, err = vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	parsedSummary, err = vm.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)
	require.True(summary.ID() == parsedSummary.ID())
	require.True(summary.Height() == parsedSummary.Height())
	require.True(bytes.Equal(summary.Bytes(), parsedSummary.Bytes()))
}

func TestStateSummaryAccept(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	reqHeight := uint64(1969)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.True(h == reqHeight)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))

	summary, err := vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	// test Accept accepted
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

	// test Accept skipped
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		return block.StateSyncSkipped, nil
	}
	status, err = summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)
}

func TestStateSummaryAcceptOlderBlock(t *testing.T) {
	require := require.New(t)

	innerVM, vm := helperBuildStateSyncTestObjects(t)
	reqHeight := uint64(1969)

	innerSummary := &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	vm.hIndexer.MarkRepaired(true)
	require.NoError(vm.SetForkHeight(innerSummary.Height() - 1))

	// Set the last accepted block height to be higher that the state summary
	// we are going to attempt to accept
	vm.lastAcceptedHeight = innerSummary.Height() + 1

	// store post fork block associated with summary
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: vm.Time(),
		HeightV:    innerSummary.Height(),
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (block.StateSummary, error) {
		require.True(h == reqHeight)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.True(bytes.Equal(b, innerBlk.Bytes()))
		return innerBlk, nil
	}

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
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   choices.Accepted,
		},
	}
	require.NoError(vm.storePostForkBlock(proBlk))

	summary, err := vm.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	// test Accept skipped
	innerSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
		return block.StateSyncStatic, nil
	}
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)
}

func TestNoStateSummariesServedWhileRepairingHeightIndex(t *testing.T) {
	require := require.New(t)

	// Note: by default proVM is built such that heightIndex will be considered complete
	coreVM, _, proVM, _, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	require.NoError(proVM.VerifyHeightIndex(context.Background()))

	// let coreVM be always ready to serve summaries
	summaryHeight := uint64(2022)
	coreStateSummary := &block.TestStateSummary{
		T:       t,
		IDV:     ids.ID{'a', 'a', 'a', 'a'},
		HeightV: summaryHeight,
		BytesV:  []byte{'c', 'o', 'r', 'e', 'S', 'u', 'm', 'm', 'a', 'r', 'y'},
	}
	coreVM.GetLastStateSummaryF = func(context.Context) (block.StateSummary, error) {
		return coreStateSummary, nil
	}
	coreVM.GetStateSummaryF = func(_ context.Context, height uint64) (block.StateSummary, error) {
		if height != summaryHeight {
			return nil, errUnknownSummary
		}
		return coreStateSummary, nil
	}

	// set height index to reindexing
	proVM.hIndexer.MarkRepaired(false)
	require.ErrorIs(proVM.VerifyHeightIndex(context.Background()), block.ErrIndexIncomplete)

	_, err := proVM.GetLastStateSummary(context.Background())
	require.ErrorIs(err, block.ErrIndexIncomplete)

	_, err = proVM.GetStateSummary(context.Background(), summaryHeight)
	require.ErrorIs(err, block.ErrIndexIncomplete)

	// declare height index complete
	proVM.hIndexer.MarkRepaired(true)
	require.NoError(proVM.VerifyHeightIndex(context.Background()))

	summary, err := proVM.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.True(summary.Height() == summaryHeight)
}
