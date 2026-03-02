// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var (
	errDuplicateVerify          = errors.New("duplicate verify")
	errUnexpectedBlockRejection = errors.New("unexpected block rejection")
)

// ProposerBlock Option interface tests section
func TestOracle_PostForkBlock_ImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			innerBlk: snowmantest.BuildChild(snowmantest.Genesis),
		},
	}

	// test
	_, err := proBlk.Options(t.Context())
	require.Equal(snowman.ErrNotOracle, err)

	// setup
	_, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	innerTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	innerOracleBlk := &TestOptionsBlock{
		Block: *innerTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(innerTestBlock),
			snowmantest.BuildChild(innerTestBlock),
		},
	}

	slb, err := block.Build(
		ids.Empty, // refer unknown parent
		time.Time{},
		0, // pChainHeight,
		block.Epoch{},
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
		},
	}

	// test
	_, err = proBlk.Options(t.Context())
	require.NoError(err)
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkBlock_PreDurango_ParentChecks(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	// create parent block ...
	parentCoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))

	// .. create child block ...
	childCoreBlk := snowmantest.BuildChild(parentCoreBlk)
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
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
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(), // refer known parent
			proVM.Time(),
			pChainHeight,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}
}

func TestBlockVerify_PostForkBlock_PostDurango_ParentChecks(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	parentCoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))
	require.NoError(proVM.waitForProposerWindow())

	childCoreBlk := snowmantest.BuildChild(parentCoreBlk)
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
		},
	}

	parentPChainHeight := parentBlk.(*postForkBlock).PChainHeight()
	nextEpoch := acp181.NewEpoch(
		proVM.Upgrades,
		parentPChainHeight,
		block.Epoch{},
		parentBlk.Timestamp(),
		proVM.Time(),
	)

	{
		// child block referring unknown parent does not verify
		childSlb, err := block.Build(
			ids.Empty, // refer unknown parent
			proVM.Time(),
			pChainHeight,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			pChainHeight,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)

		require.NoError(err)
		childBlk.SignedBlock = childSlb

		proVM.Set(childSlb.Timestamp())
		require.NoError(childBlk.Verify(t.Context()))
	}
}

func TestBlockVerify_PostForkBlock_TimestampChecks(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
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
	parentCoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))

	var (
		parentTimestamp    = parentBlk.Timestamp()
		parentPChainHeight = parentBlk.(*postForkBlock).PChainHeight()
	)

	childCoreBlk := snowmantest.BuildChild(parentCoreBlk)
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
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
			block.Epoch{},
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errTimeNotMonotonic)
	}

	blkWinDelay, err := proVM.Delay(t.Context(), childCoreBlk.Height(), parentPChainHeight, proVM.ctx.NodeID, proposer.MaxVerifyWindows)
	require.NoError(err)

	{
		// block cannot arrive before its creator window starts
		beforeWinStart := parentTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
		proVM.Clock.Set(beforeWinStart)

		childSlb, err := block.Build(
			parentBlk.ID(),
			beforeWinStart,
			pChainHeight,
			block.Epoch{},
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
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
			block.Epoch{},
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// block can arrive after its creator window starts
		afterWindowStart := parentTimestamp.Add(blkWinDelay).Add(5 * time.Second)
		proVM.Clock.Set(afterWindowStart)

		childSlb, err := block.Build(
			parentBlk.ID(),
			afterWindowStart,
			pChainHeight,
			block.Epoch{},
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// block can arrive within submission window
		atSubWindowEnd := proVM.Time().Add(proposer.MaxVerifyDelay)
		proVM.Clock.Set(atSubWindowEnd)

		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			atSubWindowEnd,
			pChainHeight,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// block timestamp cannot be too much in the future
		afterSubWinEnd := proVM.Time().Add(maxSkew).Add(time.Second)

		childSlb, err := block.Build(
			parentBlk.ID(),
			afterSubWinEnd,
			pChainHeight,
			block.Epoch{},
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errTimeTooAdvanced)
	}
}

func TestBlockVerify_PostForkBlock_PChainHeightChecks(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight / 50, nil
	}

	// create parent block ...
	parentCoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))
	require.NoError(proVM.waitForProposerWindow())

	childCoreBlk := snowmantest.BuildChild(parentCoreBlk)
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
		},
	}

	parentBlkPChainHeight := parentBlk.(*postForkBlock).PChainHeight()
	nextEpoch := acp181.NewEpoch(
		proVM.Upgrades,
		parentBlkPChainHeight,
		block.Epoch{},
		parentBlk.Timestamp(),
		proVM.Time(),
	)

	{
		// child P-Chain height must not precede parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight-1,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errPChainHeightNotMonotonic)
	}

	{
		// child P-Chain height can be equal to parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// child P-Chain height may follow parent P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			parentBlkPChainHeight,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(t.Context())
	{
		// block P-Chain height can be equal to current P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			currPChainHeight,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// block P-Chain height cannot be at higher than current P-Chain height
		childSlb, err := block.Build(
			parentBlk.ID(),
			proVM.Time(),
			currPChainHeight*2,
			nextEpoch,
			proVM.StakingCertLeaf,
			childCoreBlk.Bytes(),
			proVM.ctx.ChainID,
			proVM.StakingLeafSigner,
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errPChainHeightNotReached)
	}
}

func TestBlockVerify_PostForkBlockBuiltOnOption_PChainHeightChecks(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight / 50, nil
	}

	// create post fork oracle block ...
	innerTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *innerTestBlock,
	}
	preferredOracleBlkChild := snowmantest.BuildChild(innerTestBlock)
	oracleCoreBlk.opts = [2]*snowmantest.Block{
		preferredOracleBlkChild,
		snowmantest.BuildChild(innerTestBlock),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
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

	oracleBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(oracleBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), oracleBlk.ID()))

	// retrieve one option and verify block built on it
	require.IsType(&postForkBlock{}, oracleBlk)
	postForkOracleBlk := oracleBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(t.Context())
	require.NoError(err)
	parentBlk := opts[0]

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))

	// set VM to be ready to build next block. We set it to generate unsigned blocks
	// for simplicity.
	nextTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
	proVM.Set(nextTime)

	parentBlkPChainHeight := postForkOracleBlk.PChainHeight() // option takes proposal blocks' Pchain height

	childCoreBlk := snowmantest.BuildChild(preferredOracleBlkChild)
	childBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
		},
	}

	{
		// child P-Chain height must not precede parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight-1,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errPChainHeightNotMonotonic)
	}

	{
		// child P-Chain height can be equal to parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// child P-Chain height may follow parent P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			parentBlkPChainHeight+1,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(t.Context())
	{
		// block P-Chain height can be equal to current P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			currPChainHeight,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb

		require.NoError(childBlk.Verify(t.Context()))
	}

	{
		// block P-Chain height cannot be at higher than current P-Chain height
		childSlb, err := block.BuildUnsigned(
			parentBlk.ID(),
			nextTime,
			currPChainHeight*2,
			block.Epoch{},
			childCoreBlk.Bytes(),
		)
		require.NoError(err)
		childBlk.SignedBlock = childSlb
		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, errPChainHeightNotReached)
	}
}

func TestBlockVerify_PostForkBlock_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	require := require.New(t)

	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(builtBlk.Verify(t.Context()))

	// set error on coreBlock.Verify and recall Verify()
	coreBlk.VerifyV = errDuplicateVerify
	require.NoError(builtBlk.Verify(t.Context()))

	// rebuild a block with the same core block
	pChainHeight++
	_, err = proVM.BuildBlock(t.Context())
	require.NoError(err)
}

// ProposerBlock.Accept tests section
func TestBlockAccept_PostForkBlock_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	// setup
	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// test
	require.NoError(builtBlk.Accept(t.Context()))

	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{
			snowmantest.Genesis,
			coreBlk,
		},
	)
	acceptedID, err := proVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(builtBlk.ID(), acceptedID)
}

func TestBlockAccept_PostForkBlock_TwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	var minimumHeight uint64
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return minimumHeight, nil
	}

	// generate two blocks with the same core block and store them
	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	minimumHeight = snowmantest.GenesisHeight

	proBlk1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	minimumHeight++
	proBlk2, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NotEqual(proBlk2.ID(), proBlk1.ID())

	// set proBlk1 as preferred
	require.NoError(proBlk1.Accept(t.Context()))
	require.Equal(snowtest.Accepted, coreBlk.Status)

	acceptedID, err := proVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(proBlk1.ID(), acceptedID)
}

// ProposerBlock.Reject tests section
func TestBlockReject_PostForkBlock_InnerBlockIsNotRejected(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk.RejectV = errUnexpectedBlockRejection
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, sb)
	proBlk := sb.(*postForkBlock)

	require.NoError(proBlk.Reject(t.Context()))
}

func TestBlockVerify_PostForkBlock_ShouldBePostForkOption(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// create post fork oracle block ...
	coreTestBlk := snowmantest.BuildChild(snowmantest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(coreTestBlk),
			snowmantest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
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
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
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

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(parentBlk.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parentBlk.ID()))

	// retrieve options ...
	require.IsType(&postForkBlock{}, parentBlk)
	postForkOracleBlk := parentBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(t.Context())
	require.NoError(err)
	require.IsType(&postForkOption{}, opts[0])

	// ... and verify them the first time
	require.NoError(opts[0].Verify(t.Context()))
	require.NoError(opts[1].Verify(t.Context()))

	// Build the child
	statelessChild, err := block.Build(
		postForkOracleBlk.ID(),
		postForkOracleBlk.Timestamp().Add(proposer.WindowDuration),
		postForkOracleBlk.PChainHeight(),
		block.Epoch{},
		proVM.StakingCertLeaf,
		oracleCoreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(t.Context(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(t.Context())
	require.ErrorIs(err, errUnexpectedBlockType)
}

func TestBlockVerify_PostForkBlock_PChainTooLow(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 5)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	statelessChild, err := block.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		4,
		block.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(t.Context(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(t.Context())
	require.ErrorIs(err, errPChainHeightTooLow)
}
