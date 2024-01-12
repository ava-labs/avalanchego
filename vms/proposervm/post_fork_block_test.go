// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var errDuplicateVerify = errors.New("duplicate verify")

// ProposerBlock Option interface tests section
func TestOracle_PostForkBlock_ImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			innerBlk: &snowman.TestBlock{},
		},
	}

	// test
	_, err := proBlk.Options(context.Background())
	require.Equal(snowman.ErrNotOracle, err)

	// setup
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	_, _, proVM, _, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	innerOracleBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV: ids.Empty.Prefix(1111),
			},
			BytesV: []byte{1},
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(2222),
				},
				BytesV: []byte{2},
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(3333),
				},
				BytesV: []byte{3},
			},
		},
	}

	slb, err := block.Build(
		ids.Empty, // refer unknown parent
		time.Time{},
		0, // pChainHeight,
		proVM.StakingCertLeaf,
		innerOracleBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk = postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerOracleBlk,
			status:   choices.Processing,
		},
	}

	// test
	_, err = proBlk.Options(context.Background())
	require.NoError(err)
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkBlock_PreDurango_ParentChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime // pre Durango
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	// create parent block ...
	parentCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		ParentV: parentCoreBlk.ID(),
		BytesV:  []byte{2},
		HeightV: parentCoreBlk.Height() + 1,
	}
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	// set proVM to be able to build unsigned blocks
	proVM.Set(proVM.Time().Add(proposer.MaxVerifyDelay))

	{
		// child block referring unknown parent does not verify
		childSlb, err := block.BuildUnsigned(
			ids.Empty, // refer unknown parent
			proVM.Time(),
			pChainHeight,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(), // refer known parent
			proVM.Time(),
			pChainHeight,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}
}

func TestBlockVerify_PostForkBlock_PostDurango_ParentChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime // post Durango
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	parentCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	childCoreBlk := &snowman.TestBlock{
		ParentV: parentCoreBlk.ID(),
		BytesV:  []byte{2},
		HeightV: parentCoreBlk.Height() + 1,
	}
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	require.NoError(waitForProposerWindow(proVM, parentBlk, parentBlk.(*postForkBlock).PChainHeight()))

	{
		// child block referring unknown parent does not verify
		childSlb, err := block.Build(
			ids.Empty, // refer unknown parent
			proVM.Time(),
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)

		require.NoError(err)
		childBlk.SignedBlock = childSlb

		proVM.Set(childSlb.Timestamp())
		require.NoError(childBlk.Verify(context.Background()))
	}
}

func TestBlockVerify_PostForkBlock_TimestampChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// reduce validator state to allow proVM.ctx.NodeID to be easily selected as proposer
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			thisNode = proVM.ctx.NodeID
			nodeID1  = ids.BuildTestNodeID([]byte{1})
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			thisNode: {
				NodeID: thisNode,
				Weight: 5,
			},
			nodeID1: {
				NodeID: nodeID1,
				Weight: 100,
			},
		}, nil
	}
	proVM.ctx.ValidatorState = valState

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	// create parent block ...
	parentCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	var (
		parentTimestamp    = parentBlk.Timestamp()
		parentPChainHeight = parentBlk.(*postForkBlock).PChainHeight()
	)

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV: parentCoreBlk.ID(),
		HeightV: parentCoreBlk.Height() + 1,
		BytesV:  []byte{2},
	}
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	{
		// child block timestamp cannot be lower than parent timestamp
		newTime := parentTimestamp.Add(-1 * time.Second)
		proVM.Clock.Set(newTime)

		childSlb, err := block.Build(
			parentBlk.ID(),
			newTime,
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errTimeNotMonotonic)
	}

	blkWinDelay, err := proVM.Delay(context.Background(), childCoreBlk.Height(), parentPChainHeight, proVM.ctx.NodeID, proposer.MaxVerifyWindows)
	require.NoError(err)

	{
		// block cannot arrive before its creator window starts
		beforeWinStart := parentTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
		proVM.Clock.Set(beforeWinStart)

		childSlb, err := block.Build(
			parentBlk.ID(),
			beforeWinStart,
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// block can arrive at its creator window starts
		atWindowStart := parentTimestamp.Add(blkWinDelay)
		proVM.Clock.Set(atWindowStart)

		childSlb, err := block.Build(
			parentBlk.ID(),
			atWindowStart,
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// block can arrive after its creator window starts
		afterWindowStart := parentTimestamp.Add(blkWinDelay).Add(5 * time.Second)
		proVM.Clock.Set(afterWindowStart)

		childSlb, err := block.Build(
			parentBlk.ID(),
			afterWindowStart,
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// block can arrive within submission window
		atSubWindowEnd := proVM.Time().Add(proposer.MaxVerifyDelay)
		proVM.Clock.Set(atSubWindowEnd)

		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			atSubWindowEnd,
			pChainHeight,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// block timestamp cannot be too much in the future
		afterSubWinEnd := proVM.Time().Add(maxSkew).Add(time.Second)

		childSlb, err := block.Build(
			parentBlk.ID(),
			afterSubWinEnd,
			pChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errTimeTooAdvanced)
	}
}

func TestBlockVerify_PostForkBlock_PChainHeightChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight / 50, nil
	}

	// create parent block ...
	parentCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// set VM to be ready to build next block. We set it to generate unsigned blocks
	// for simplicity.
	parentBlkPChainHeight := parentBlk.(*postForkBlock).PChainHeight()
	require.NoError(waitForProposerWindow(proVM, parentBlk, parentBlkPChainHeight))

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV: parentCoreBlk.ID(),
		HeightV: parentBlk.Height() + 1,
		BytesV:  []byte{2},
	}
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	{
		// child P-Chain height must not precede parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight-1,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errPChainHeightNotMonotonic)
	}

	{
		// child P-Chain height can be equal to parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// child P-Chain height may follow parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	{
		// block P-Chain height can be equal to current P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			currPChainHeight,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// block P-Chain height cannot be at higher than current P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			currPChainHeight*2,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errPChainHeightNotReached)
	}
}

func TestBlockVerify_PostForkBlockBuiltOnOption_PChainHeightChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight / 50, nil
	}

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{1},
			ParentV: coreGenBlk.ID(),
			HeightV: coreGenBlk.Height() + 1,
		},
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{2},
			ParentV: oracleCoreBlk.ID(),
			HeightV: oracleCoreBlk.Height() + 1,
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{3},
			ParentV: oracleCoreBlk.ID(),
			HeightV: oracleCoreBlk.Height() + 1,
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	oracleBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(oracleBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), oracleBlk.ID()))

	// retrieve one option and verify block built on it
	require.IsType(&postForkBlock{}, oracleBlk)
	postForkOracleBlk := oracleBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)
	parentBlk := opts[0]

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// set VM to be ready to build next block. We set it to generate unsigned blocks
	// for simplicity.
	nextTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
	proVM.Set(nextTime)

	parentBlkPChainHeight := postForkOracleBlk.PChainHeight() // option takes proposal blocks' Pchain height

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV: oracleCoreBlk.opts[0].ID(),
		BytesV:  []byte{2},
		HeightV: oracleCoreBlk.opts[0].Height() + 1,
	}
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	{
		// child P-Chain height must not precede parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight-1,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errPChainHeightNotMonotonic)
	}

	{
		// child P-Chain height can be equal to parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// child P-Chain height may follow parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight+1,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	{
		// block P-Chain height can be equal to current P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			currPChainHeight,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(context.Background()))
	}

	{
		// block P-Chain height cannot be at higher than current P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			currPChainHeight*2,
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb
		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, errPChainHeightNotReached)
	}
}

func TestBlockVerify_PostForkBlock_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	require := require.New(t)

	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(builtBlk.Verify(context.Background()))

	// set error on coreBlock.Verify and recall Verify()
	coreBlk.VerifyV = errDuplicateVerify
	require.NoError(builtBlk.Verify(context.Background()))

	// rebuild a block with the same core block
	pChainHeight++
	_, err = proVM.BuildBlock(context.Background())
	require.NoError(err)
}

// ProposerBlock.Accept tests section
func TestBlockAccept_PostForkBlock_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	// setup
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// test
	require.NoError(builtBlk.Accept(context.Background()))

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if coreBlk.Status() == choices.Accepted {
			return coreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(builtBlk.ID(), acceptedID)
}

func TestBlockAccept_PostForkBlock_TwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	var minimumHeight uint64
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return minimumHeight, nil
	}

	// generate two blocks with the same core block and store them
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	minimumHeight = coreGenBlk.Height()

	proBlk1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	minimumHeight++
	proBlk2, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NotEqual(proBlk2.ID(), proBlk1.ID())

	// set proBlk1 as preferred
	require.NoError(proBlk1.Accept(context.Background()))
	require.Equal(choices.Accepted, coreBlk.Status())

	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(proBlk1.ID(), acceptedID)
}

// ProposerBlock.Reject tests section
func TestBlockReject_PostForkBlock_InnerBlockIsNotRejected(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, sb)
	proBlk := sb.(*postForkBlock)

	require.NoError(proBlk.Reject(context.Background()))

	require.Equal(choices.Rejected, proBlk.Status())
	require.NotEqual(choices.Rejected, proBlk.innerBlk.Status())
}

func TestBlockVerify_PostForkBlock_ShouldBePostForkOption(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{1},
			ParentV: coreGenBlk.ID(),
			HeightV: coreGenBlk.Height() + 1,
		},
	}
	coreOpt0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: oracleCoreBlk.ID(),
		HeightV: oracleCoreBlk.Height() + 1,
	}
	coreOpt1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: oracleCoreBlk.ID(),
		HeightV: oracleCoreBlk.Height() + 1,
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		coreOpt0,
		coreOpt1,
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// retrieve options ...
	require.IsType(&postForkBlock{}, parentBlk)
	postForkOracleBlk := parentBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.IsType(&postForkOption{}, opts[0])

	// ... and verify them the first time
	require.NoError(opts[0].Verify(context.Background()))
	require.NoError(opts[1].Verify(context.Background()))

	// Build the child
	statelessChild, err := block.Build(
		postForkOracleBlk.ID(),
		postForkOracleBlk.Timestamp().Add(proposer.WindowDuration),
		postForkOracleBlk.PChainHeight(),
		proVM.StakingCertLeaf,
		oracleCoreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

func TestBlockVerify_PostForkBlock_PChainTooLow(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, durangoTime, 5)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	statelessChild, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		4,
		coreBlk.Bytes(),
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.ErrorIs(err, errPChainHeightTooLow)
}
