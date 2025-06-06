// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
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
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
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

// We consider cases where this node is not current proposer (may be scheduled
// in the next future or not).
//
// We check that scheduler is called nonetheless, to be able to process innerVM
// block requests.
func TestBuildChildResetScheduler(t *testing.T) {
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
	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Latest),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        prometheus.NewRegistry(),
		},
		ChainVM: blockmock.NewChainVM(ctrl),
		ctx: &snow.Context{
			NodeID:         thisNodeID,
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
		Windower:               windower,
		Scheduler:              scheduler,
		proposerBuildSlotGauge: prometheus.NewGauge(prometheus.GaugeOpts{}),
	}
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
