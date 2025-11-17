// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestOracle_PreForkBlkImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := preForkBlock{
		Block: snowmantest.BuildChild(snowmantest.Genesis),
	}

	// test
	_, err := proBlk.Options(t.Context())
	require.Equal(snowman.ErrNotOracle, err)

	// setup
	proBlk = preForkBlock{
		Block: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options(t.Context())
	require.NoError(err)
}

func TestOracle_PreForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// create pre fork oracle block ...
	coreTestBlk := snowmantest.BuildChild(snowmantest.Genesis)
	preferredTestBlk := snowmantest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*snowmantest.Block{
			preferredTestBlk,
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

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(t.Context())
	require.NoError(err)
	require.NoError(opts[0].Verify(t.Context()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(t.Context(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		Block: *snowmantest.BuildChild(preferredTestBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return lastCoreBlk, nil
	}

	preForkChild, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)
}

func TestOracle_PostForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// create pre fork oracle block pre activation time...
	coreTestBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreTestBlk.TimestampV = activationTime.Add(-1 * time.Second)

	// ... whose options are post activation time
	preferredBlk := snowmantest.BuildChild(coreTestBlk)
	preferredBlk.TimestampV = activationTime.Add(time.Second)

	unpreferredBlk := snowmantest.BuildChild(coreTestBlk)
	unpreferredBlk.TimestampV = activationTime.Add(time.Second)

	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*snowmantest.Block{
			preferredBlk,
			unpreferredBlk,
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

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(t.Context())
	require.NoError(err)
	require.NoError(opts[0].Verify(t.Context()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(t.Context(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		Block: *snowmantest.BuildChild(preferredBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return lastCoreBlk, nil
	}

	postForkChild, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)
}

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

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
			return nil, database.ErrNotFound
		}
	}

	parentBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// .. create child block ...
	childCoreBlk := snowmantest.BuildChild(parentCoreBlk)
	childBlk := preForkBlock{
		Block: childCoreBlk,
		vm:    proVM,
	}

	{
		// child block referring unknown parent does not verify
		childCoreBlk.ParentV = ids.Empty
		err = childBlk.Verify(t.Context())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childCoreBlk.ParentV = parentBlk.ID()
		require.NoError(childBlk.Verify(t.Context()))
	}
}

func TestBlockVerify_BlocksBuiltOnPreForkGenesis(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	preActivationTime := activationTime.Add(-1 * time.Second)
	proVM.Set(preActivationTime)

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk.TimestampV = preActivationTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// preFork block verifies if parent is before fork activation time
	preForkChild, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)

	require.NoError(preForkChild.Verify(t.Context()))

	// postFork block does NOT verify if parent is before fork activation time
	postForkStatelessChild, err := statelessblock.Build(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		0, // pChainHeight
		statelessblock.Epoch{},
		proVM.StakingCertLeaf,
		coreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	postForkChild := &postForkBlock{
		SignedBlock: postForkStatelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: coreBlk,
		},
	}

	require.True(postForkChild.Timestamp().Before(activationTime))
	err = postForkChild.Verify(t.Context())
	require.ErrorIs(err, errProposersNotActivated)

	// once activation time is crossed postForkBlock are produced
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreVM.SetPreferenceF = func(context.Context, ids.ID) error {
		return nil
	}
	require.NoError(proVM.SetPreference(t.Context(), preForkChild.ID()))

	secondCoreBlk := snowmantest.BuildChild(coreBlk)
	secondCoreBlk.TimestampV = postActivationTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return secondCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	lastPreForkBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, lastPreForkBlk)

	require.NoError(lastPreForkBlk.Verify(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), lastPreForkBlk.ID()))
	thirdCoreBlk := snowmantest.BuildChild(secondCoreBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return thirdCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case secondCoreBlk.ID():
			return secondCoreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	firstPostForkBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, firstPostForkBlk)

	require.NoError(firstPostForkBlk.Verify(t.Context()))
}

func TestBlockVerify_BlocksBuiltOnPostForkGenesis(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(-1 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	proVM.Set(activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// build parent block after fork activation time ...
	coreBlock := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlock, nil
	}

	// postFork block verifies if parent is after fork activation time
	postForkChild, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)

	require.NoError(postForkChild.Verify(t.Context()))

	// preFork block does NOT verify if parent is after fork activation time
	preForkChild := preForkBlock{
		Block: coreBlock,
		vm:    proVM,
	}
	err = preForkChild.Verify(t.Context())
	require.ErrorIs(err, errUnexpectedBlockType)
}

func TestBlockAccept_PreFork_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	// setup
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

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

// ProposerBlock.Reject tests section
func TestBlockReject_PreForkBlock_InnerBlockIsRejected(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, sb)
	proBlk := sb.(*preForkBlock)

	require.NoError(proBlk.Reject(t.Context()))
	require.Equal(snowtest.Rejected, coreBlk.Status)
}

func TestBlockVerify_ForkBlockIsOracleBlock(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(coreTestBlk),
			snowmantest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(t.Context(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(t.Context()))

	oracleBlock, ok := firstBlock.(snowman.OracleBlock)
	require.True(ok)

	options, err := oracleBlock.Options(t.Context())
	require.NoError(err)

	require.NoError(options[0].Verify(t.Context()))

	require.NoError(options[1].Verify(t.Context()))
}

func TestBlockVerify_ForkBlockIsOracleBlockButChildrenAreSigned(t *testing.T) {
	require := require.New(t)

	activationTime := snowmantest.GenesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0, activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(coreTestBlk),
			snowmantest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(t.Context(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(t.Context()))

	slb, err := statelessblock.Build(
		firstBlock.ID(), // refer unknown parent
		firstBlock.Timestamp(),
		0, // pChainHeight,
		statelessblock.Epoch{},
		proVM.StakingCertLeaf,
		coreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(t.Context(), slb.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(t.Context())
	require.ErrorIs(err, errUnexpectedBlockType)
}

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we only call the VM's BuildBlockWithContext
// when a P-chain height can be correctly provided from the parent block.
func TestPreForkBlock_BuildBlockWithContext(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pChainHeight := uint64(1337)
	blkID := ids.GenerateTestID()
	innerBlk := snowmanmock.NewBlock(ctrl)
	innerBlk.EXPECT().ID().Return(blkID).AnyTimes()
	innerBlk.EXPECT().Timestamp().Return(upgrade.UnscheduledActivationTime)
	builtBlk := snowmanmock.NewBlock(ctrl)
	builtBlk.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
	builtBlk.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
	builtBlk.EXPECT().Height().Return(pChainHeight).AnyTimes()
	innerVM := blockmock.NewChainVM(ctrl)
	innerVM.EXPECT().BuildBlock(gomock.Any()).Return(builtBlk, nil).AnyTimes()
	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(t.Context()).Return(pChainHeight, nil).AnyTimes()

	vm := &VM{
		ChainVM: innerVM,
		ctx: &snow.Context{
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
	}

	blk := &preForkBlock{
		Block: innerBlk,
		vm:    vm,
	}

	// Should call BuildBlock since proposervm won't have a P-chain height
	gotChild, err := blk.buildChild(t.Context())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)

	// Should call BuildBlock since proposervm is not activated
	innerBlk.EXPECT().Timestamp().Return(time.Time{})
	vm.Upgrades.ApricotPhase4Time = upgrade.UnscheduledActivationTime

	gotChild, err = blk.buildChild(t.Context())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*preForkBlock).Block)
}

func TestPreForkBlock_NonZeroEpoch(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	firstBlockTime := snowmantest.GenesisTimestamp.Add(time.Second)

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk.TimestampV = firstBlockTime

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

	proVM.Set(firstBlockTime)
	// Create a post-fork child block with a non-zero epoch
	nonZeroEpoch := statelessblock.Epoch{
		PChainHeight: 100,
		Number:       1,
		StartTime:    firstBlockTime.Unix(),
	}

	postForkStatelessChild, err := statelessblock.Build(
		snowmantest.GenesisID,
		firstBlockTime,
		100,
		nonZeroEpoch,
		proVM.StakingCertLeaf,
		coreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)

	postForkChild := &postForkBlock{
		SignedBlock: postForkStatelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: coreBlk,
		},
	}

	// Verify that the child block is rejected due to non-zero epoch
	err = postForkChild.Verify(t.Context())
	require.ErrorIs(err, errEpochNotZero)
}
