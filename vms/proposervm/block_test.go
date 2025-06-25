// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer/proposermock"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler/schedulermock"
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
	vdrState.EXPECT().GetMinimumHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

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
		context.Background(),
		parentID,
		parentTimestamp,
		pChainHeight-1,
	)
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)
}

func TestPreDurangoValidatorNodeBlockBuiltDelaysTests(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
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
	ctx := context.Background()

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
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

// We consider cases where this node is not current proposer (may be scheduled in the next future or not).
// We check that scheduler is called nonetheless, to be able to process innerVM block requests
func TestPostDurangoBuildChildResetScheduler(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		thisNodeID              = ids.GenerateTestNodeID()
		selectedProposer        = ids.GenerateTestNodeID()
		pChainHeight     uint64 = 1337
		parentID                = ids.GenerateTestID()
		parentTimestamp         = time.Now().Truncate(time.Second)
		now                     = parentTimestamp.Add(12 * time.Second)
		parentHeight     uint64 = 1234
	)

	innerBlk := snowmanmock.NewBlock(ctrl)
	innerBlk.EXPECT().Height().Return(parentHeight + 1).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

	windower := proposermock.NewWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(selectedProposer, nil).AnyTimes() // return a proposer different from thisNode, to check whether scheduler is reset

	scheduler := schedulermock.NewScheduler(ctrl)

	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)

	chainVM := blockmock.NewChainVM(ctrl)
	chainVM.EXPECT().WaitForEvent(gomock.Any()).Return(common.PendingTxs, nil).AnyTimes()

	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Latest),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        prometheus.NewRegistry(),
		},
		ChainVM: chainVM,
		ctx: &snow.Context{
			NodeID:         thisNodeID,
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
		Windower:               windower,
		Scheduler:              scheduler,
		proposerBuildSlotGauge: prometheus.NewGauge(prometheus.GaugeOpts{}),
	}

	vm.subscriber = common.NewSubscriptionProxy(vm.ChainVM.WaitForEvent, &logging.NoLog{})
	defer vm.subscriber.Close()

	vm.Clock.Set(now)

	blk := &postForkCommonComponents{
		innerBlk: innerBlk,
		vm:       vm,
	}

	delays := []time.Duration{
		proposer.MaxLookAheadWindow - time.Minute,
		proposer.MaxLookAheadWindow,
		proposer.MaxLookAheadWindow + time.Minute,
	}

	for _, delay := range delays {
		windower.EXPECT().MinDelayForProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(delay, nil).Times(1)

		// we mock the scheduler setting the exact time we expect it to be reset
		// to
		expectedSchedulerTime := parentTimestamp.Add(delay)
		scheduler.EXPECT().SetBuildBlockTime(expectedSchedulerTime).Times(1)

		_, err = blk.buildChild(
			context.Background(),
			parentID,
			parentTimestamp,
			pChainHeight-1,
		)
		require.ErrorIs(err, errUnexpectedProposer)
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
	)

	innerParentBlock := snowmantest.Genesis
	innerChildBlock := snowmantest.BuildChild(innerParentBlock)

	innerBlockBuilderVM := blockmock.NewBuildBlockWithContextChainVM(ctrl)
	// Expect the that context passed in has parent's P-Chain height
	innerBlockBuilderVM.EXPECT().BuildBlockWithContext(gomock.Any(), &block.Context{
		PChainHeight: parentPChainHeght,
	}).Return(innerChildBlock, nil).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

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
		context.Background(),
		parentID,
		parentTimestamp,
		parentPChainHeght,
	)
	require.NoError(err)
	require.Equal(innerChildBlock, gotChild.(*postForkBlock).innerBlk)
}
