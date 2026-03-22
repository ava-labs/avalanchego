// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer/proposermock"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we call the VM's BuildBlockWithContext
// method to build a block rather than BuildBlockWithContext. If the proposervm
// isn't activated, we should call BuildBlock rather than BuildBlockWithContext.
func TestPostForkCommonComponents_buildChild(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		nodeID                 = ids.GenerateTestNodeID()
		pChainHeight    uint64 = 1337
		parentID               = ids.GenerateTestID()
		parentTimestamp        = time.Now().Truncate(time.Second)
		parentHeight    uint64 = 1234
		blkID                  = ids.GenerateTestID()
		parentEpoch            = statelessblock.Epoch{}
	)

	innerBlk := snowmanmock.NewBlock(ctrl)
	innerBlk.EXPECT().ID().Return(blkID).AnyTimes()
	innerBlk.EXPECT().Height().Return(parentHeight + 1).AnyTimes()

	builtBlk := snowmanmock.NewBlock(ctrl)
	builtBlk.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
	builtBlk.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
	builtBlk.EXPECT().Height().Return(pChainHeight).AnyTimes()

	innerVM := blockmock.NewChainVM(ctrl)
	innerBlockBuilderVM := blockmock.NewBuildBlockWithContextChainVM(ctrl)
	innerBlockBuilderVM.EXPECT().BuildBlockWithContext(gomock.Any(), &block.Context{
		PChainHeight: pChainHeight,
	}).Return(builtBlk, nil).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(t.Context()).Return(pChainHeight, nil).AnyTimes()

	windower := proposermock.NewWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nodeID, nil).AnyTimes()

	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)
	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Latest),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        prometheus.NewRegistry(),
		},
		ChainVM:        innerVM,
		blockBuilderVM: innerBlockBuilderVM,
		ctx: &snow.Context{
			NodeID:         nodeID,
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
		Windower: windower,
	}

	blk := &postForkCommonComponents{
		innerBlk: innerBlk,
		vm:       vm,
	}

	// Should call BuildBlockWithContext since proposervm is activated
	gotChild, err := blk.buildChild(
		t.Context(),
		parentID,
		parentTimestamp,
		pChainHeight,
		parentEpoch,
	)
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)
}

func TestPreDurangoValidatorNodeBlockBuiltDelaysTests(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(ctx))
	}()

	// Build a post fork block. It'll be the parent block in our test cases
	parentTime := time.Now().Truncate(time.Second)
	proVM.Set(parentTime)

	coreParentBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(err)
	require.NoError(parentBlk.Verify(ctx))
	require.NoError(parentBlk.Accept(ctx))

	// Make sure preference is duly set
	require.NoError(proVM.SetPreference(ctx, parentBlk.ID()))
	require.Equal(proVM.preferred, parentBlk.ID())
	_, err = proVM.getPostForkBlock(ctx, parentBlk.ID())
	require.NoError(err)

	// Force this node to be the only validator, so to guarantee
	// it'd be picked if block build time was before MaxVerifyDelay
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		// a validator with a weight large enough to fully fill the proposers list
		weight := uint64(proposer.MaxBuildWindows * 2)

		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: weight,
			},
		}, nil
	}

	coreChildBlk := snowmantest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreChildBlk, nil
	}

	{
		// Set local clock before MaxVerifyDelay from parent timestamp.
		// Check that child block is signed.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay - time.Second)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(proVM.ctx.NodeID, childBlk.Proposer()) // signed block
	}

	{
		// Set local clock exactly MaxVerifyDelay from parent timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}

	{
		// Set local clock between MaxVerifyDelay and MaxBuildDelay from parent
		// timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add((proposer.MaxVerifyDelay + proposer.MaxBuildDelay) / 2)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}

	{
		// Set local clock after MaxBuildDelay from parent timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add(proposer.MaxBuildDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}
}

func TestPreDurangoNonValidatorNodeBlockBuiltDelaysTests(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(ctx))
	}()

	// Build a post fork block. It'll be the parent block in our test cases
	parentTime := time.Now().Truncate(time.Second)
	proVM.Set(parentTime)

	coreParentBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(err)
	require.NoError(parentBlk.Verify(ctx))
	require.NoError(parentBlk.Accept(ctx))

	// Make sure preference is duly set
	require.NoError(proVM.SetPreference(ctx, parentBlk.ID()))
	require.Equal(proVM.preferred, parentBlk.ID())
	_, err = proVM.getPostForkBlock(ctx, parentBlk.ID())
	require.NoError(err)

	// Mark node as non validator
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			aValidator = ids.GenerateTestNodeID()

			// a validator with a weight large enough to fully fill the proposers list
			weight = uint64(proposer.MaxBuildWindows * 2)
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			aValidator: {
				NodeID: aValidator,
				Weight: weight,
			},
		}, nil
	}

	coreChildBlk := snowmantest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreChildBlk, nil
	}

	{
		// Set local clock before MaxVerifyDelay from parent timestamp.
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay - time.Second)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock exactly MaxVerifyDelay from parent timestamp.
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock among MaxVerifyDelay and MaxBuildDelay from parent timestamp
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add((proposer.MaxVerifyDelay + proposer.MaxBuildDelay) / 2)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock after MaxBuildDelay from parent timestamp
		// Check that child block is built and it is unsigned
		localTime := parentBlk.Timestamp().Add(proposer.MaxBuildDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}
}

// Confirm that prior to Etna activation, the P-chain height passed to the
// VM building the inner block is P-Chain height of the parent block.
func TestPreEtnaContextPChainHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		nodeID                   = ids.GenerateTestNodeID()
		pChainHeight      uint64 = 1337
		parentPChainHeght        = pChainHeight - 1
		parentID                 = ids.GenerateTestID()
		parentTimestamp          = time.Now().Truncate(time.Second)
		parentEpoch              = statelessblock.Epoch{}
	)

	innerParentBlock := snowmantest.Genesis
	innerChildBlock := snowmantest.BuildChild(innerParentBlock)

	innerBlockBuilderVM := blockmock.NewBuildBlockWithContextChainVM(ctrl)
	// Expect the that context passed in has parent's P-Chain height
	innerBlockBuilderVM.EXPECT().BuildBlockWithContext(gomock.Any(), &block.Context{
		PChainHeight: parentPChainHeght,
	}).Return(innerChildBlock, nil).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(t.Context()).Return(pChainHeight, nil).AnyTimes()

	windower := proposermock.NewWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nodeID, nil).AnyTimes()

	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Durango), // Use Durango for pre-Etna behavior
			StakingCertLeaf:   pTestCert,
			StakingLeafSigner: pTestSigner,
			Registerer:        prometheus.NewRegistry(),
		},
		blockBuilderVM: innerBlockBuilderVM,
		ctx: &snow.Context{
			NodeID:         nodeID,
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
		Windower: windower,
	}

	blk := &postForkCommonComponents{
		innerBlk: innerChildBlock,
		vm:       vm,
	}

	// Should call BuildBlockWithContext since proposervm is activated
	gotChild, err := blk.buildChild(
		t.Context(),
		parentID,
		parentTimestamp,
		parentPChainHeght,
		parentEpoch,
	)
	require.NoError(err)
	require.Equal(innerChildBlock, gotChild.(*postForkBlock).innerBlk)
}

// Confirm that VM rejects blocks with non-zero epoch prior to granite upgrade activation
func TestPreGraniteBlock_NonZeroEpoch(t *testing.T) {
	require := require.New(t)

	_, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	innerBlk := snowmantest.BuildChild(snowmantest.Genesis)
	slb, err := statelessblock.Build(
		proVM.preferred,
		proVM.Time(),
		100, // pChainHeight,
		statelessblock.Epoch{
			PChainHeight: 1,
			Number:       1,
			StartTime:    1,
		},
		proVM.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
		},
	}
	err = proBlk.Verify(t.Context())
	require.ErrorIs(err, errEpochNotZero)
}

// Verify that post-fork blocks are validated to contain the correct epoch
// information.
func TestPostGraniteBlock_EpochMatches(t *testing.T) {
	ctx := t.Context()

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(t, proVM.Shutdown(ctx))
	}()

	coreParentBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreChildBlk := snowmantest.BuildChild(coreParentBlk)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, coreChildBlk.Bytes()):
			return coreChildBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreParentBlk, nil
	}

	// Build the first proposervm block so that verification is on top of a
	// post-fork block.
	parentTime := upgrade.InitiallyActiveTime.Add(24 * time.Hour) // Some arbitrary time after initial activations
	proVM.Set(parentTime)

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, parentBlk.Verify(ctx))
	require.NoError(t, proVM.SetPreference(ctx, parentBlk.ID()))
	require.NoError(t, proVM.waitForProposerWindow())

	tests := []struct {
		name    string
		epoch   statelessblock.Epoch
		wantErr error
	}{
		{
			name: "valid",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: nil,
		},
		{
			name:    "missing_epoch",
			epoch:   statelessblock.Epoch{},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_p_chain_height",
			epoch: statelessblock.Epoch{
				PChainHeight: 1,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_number",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       2,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_start_time",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix() + 1,
			},
			wantErr: errEpochMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			statelessBlock, err := statelessblock.Build(
				parentBlk.ID(),
				proVM.Time(),
				defaultPChainHeight,
				test.epoch,
				proVM.StakingCertLeaf,
				coreChildBlk.Bytes(),
				proVM.ctx.ChainID,
				proVM.StakingLeafSigner,
			)
			require.NoError(err)

			blockBytes := statelessBlock.Bytes()
			block, err := proVM.ParseBlock(ctx, blockBytes)
			require.NoError(err)

			err = block.Verify(ctx)
			require.ErrorIs(err, test.wantErr)
		})
	}
}

func TestDBClosedDuringVerifyLogsWarnNotError(t *testing.T) {
	tests := []struct {
		name                   string
		getCurrentHeightClosed bool
		expectedProposerClosed bool
	}{
		{
			name:                   "GetCurrentHeight returns ErrClosed",
			getCurrentHeightClosed: true,
		},
		{
			name:                   "ExpectedProposer returns ErrClosed",
			expectedProposerClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			pChainHeight := uint64(100)
			parentTimestamp := time.Now().Truncate(time.Second)
			chainID := ids.GenerateTestID()
			parentInnerBlkID := ids.GenerateTestID()

			parentInnerBlk := snowmanmock.NewBlock(ctrl)
			parentInnerBlk.EXPECT().ID().Return(parentInnerBlkID).AnyTimes()

			childInnerBlk := snowmanmock.NewBlock(ctrl)
			childInnerBlk.EXPECT().Parent().Return(parentInnerBlkID).AnyTimes()
			childInnerBlk.EXPECT().Height().Return(uint64(10)).AnyTimes()

			upgrades := upgradetest.GetConfig(upgradetest.Latest)
			childSlb, err := statelessblock.Build(
				ids.GenerateTestID(),
				parentTimestamp,
				pChainHeight,
				acp181.NewEpoch(upgrades, pChainHeight, statelessblock.Epoch{}, parentTimestamp, parentTimestamp),
				pTestCert,
				[]byte{1, 2, 3},
				chainID,
				pTestSigner,
			)
			require.NoError(err)

			logger := &levelRecordingLogger{}
			vdrState := validatorsmock.NewState(ctrl)
			if tt.getCurrentHeightClosed {
				vdrState.EXPECT().GetCurrentHeight(gomock.Any()).Return(uint64(0), database.ErrClosed)
			} else {
				vdrState.EXPECT().GetCurrentHeight(gomock.Any()).Return(pChainHeight, nil)
			}

			windower := proposermock.NewWindower(ctrl)
			if tt.expectedProposerClosed {
				windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(ids.EmptyNodeID, database.ErrClosed)
			}

			vm := &VM{
				Config: Config{
					Upgrades: upgrades,
				},
				ctx: &snow.Context{
					NodeID:         ids.NodeIDFromCert(pTestCert),
					ValidatorState: vdrState,
					Log:            logger,
					ChainID:        chainID,
				},
				Windower:       windower,
				consensusState: snow.NormalOp,
			}
			vm.Clock.Set(parentTimestamp.Add(time.Second))

			parent := &postForkCommonComponents{vm: vm, innerBlk: parentInnerBlk}
			child := &postForkBlock{
				SignedBlock: childSlb,
				postForkCommonComponents: postForkCommonComponents{
					vm:       vm,
					innerBlk: childInnerBlk,
				},
			}

			err = parent.Verify(t.Context(), parentTimestamp, pChainHeight, statelessblock.Epoch{}, child)
			require.ErrorIs(err, database.ErrClosed)
			require.Equal("warn", logger.lastLevel, "database.ErrClosed should log at Warn level, not Error")
		})
	}
}

func TestDBClosedDuringBuildChildLogsWarnNotError(t *testing.T) {
	tests := []struct {
		name                   string
		getMinHeightClosed     bool
		expectedProposerClosed bool
	}{
		{
			name:               "GetMinimumHeight returns ErrClosed",
			getMinHeightClosed: true,
		},
		{
			name:                   "ExpectedProposer returns ErrClosed",
			expectedProposerClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			pChainHeight := uint64(100)
			parentTimestamp := time.Now().Truncate(time.Second)

			logger := &levelRecordingLogger{}
			vdrState := validatorsmock.NewState(ctrl)
			if tt.getMinHeightClosed {
				vdrState.EXPECT().GetMinimumHeight(gomock.Any()).Return(uint64(0), database.ErrClosed)
			} else {
				vdrState.EXPECT().GetMinimumHeight(gomock.Any()).Return(pChainHeight, nil)
			}

			windower := proposermock.NewWindower(ctrl)
			if tt.expectedProposerClosed {
				windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(ids.EmptyNodeID, database.ErrClosed)
			}

			innerBlk := snowmanmock.NewBlock(ctrl)
			innerBlk.EXPECT().Height().Return(uint64(10)).AnyTimes()

			vm := &VM{
				Config: Config{
					Upgrades: upgradetest.GetConfig(upgradetest.Latest),
				},
				ctx: &snow.Context{
					NodeID:         ids.NodeIDFromCert(pTestCert),
					ValidatorState: vdrState,
					Log:            logger,
				},
				Windower: windower,
			}
			vm.Clock.Set(parentTimestamp.Add(time.Second))

			blk := &postForkCommonComponents{innerBlk: innerBlk, vm: vm}
			_, err := blk.buildChild(t.Context(), ids.GenerateTestID(), parentTimestamp, pChainHeight, statelessblock.Epoch{})
			require.ErrorIs(err, database.ErrClosed)
			require.Equal("warn", logger.lastLevel, "database.ErrClosed should log at Warn level, not Error")
		})
	}
}
