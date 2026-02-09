// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmanmock"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	_ block.ChainVM                         = (*fullVM)(nil)
	_ block.StateSyncableVM                 = (*fullVM)(nil)
	_ block.SetPreferenceWithContextChainVM = (*fullVM)(nil)
)

type fullVM struct {
	*blocktest.VM
	*blocktest.StateSyncableVM
	*blocktest.SetPreferenceVM
}

var (
	pTestSigner crypto.Signer
	pTestCert   *staking.Certificate

	defaultPChainHeight uint64 = 2000

	errUnknownBlock      = errors.New("unknown block")
	errUnverifiedBlock   = errors.New("unverified block")
	errMarshallingFailed = errors.New("marshalling failed")
	errTooHigh           = errors.New("too high")
	errUnexpectedCall    = errors.New("unexpected call")
)

func init() {
	tlsCert, err := staking.NewTLSCert()
	if err != nil {
		panic(err)
	}
	pTestSigner = tlsCert.PrivateKey.(crypto.Signer)
	pTestCert, err = staking.ParseCertificate(tlsCert.Leaf.Raw)
	if err != nil {
		panic(err)
	}
}

// initTestProposerVM creates a proposerVM for testing.
// If forkActivationTime is provided, the fork activates at that specific time.
// If not provided, the fork is already activated at InitiallyActiveTime.
func initTestProposerVM(
	t *testing.T,
	fork upgradetest.Fork,
	minPChainHeight uint64,
	forkActivationTime ...time.Time,
) (
	*fullVM,
	*validatorstest.State,
	*VM,
	database.Database,
) {
	require := require.New(t)

	initialState := []byte("genesis state")
	coreVM := &fullVM{
		VM: &blocktest.VM{
			VM: enginetest.VM{
				T: t,
				InitializeF: func(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, []*common.Fx, common.AppSender) error {
					return nil
				},
			},
			LastAcceptedF: snowmantest.MakeLastAcceptedBlockF(
				[]*snowmantest.Block{snowmantest.Genesis},
			),
			GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
				switch blkID {
				case snowmantest.GenesisID:
					return snowmantest.Genesis, nil
				default:
					return nil, errUnknownBlock
				}
			},
			ParseBlockF: func(_ context.Context, b []byte) (snowman.Block, error) {
				switch {
				case bytes.Equal(b, snowmantest.GenesisBytes):
					return snowmantest.Genesis, nil
				default:
					return nil, errUnknownBlock
				}
			},
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
		},
		SetPreferenceVM: &blocktest.SetPreferenceVM{
			T: t,
		},
	}
	// Default to routing SetPreferenceWithContext to SetPreference
	coreVM.SetPreferenceWithContextF = func(ctx context.Context, blkID ids.ID, _ *block.Context) error {
		return coreVM.SetPreference(ctx, blkID)
	}

	var upgrades upgrade.Config
	if len(forkActivationTime) > 0 {
		upgrades = upgradetest.GetConfigWithUpgradeTime(fork, forkActivationTime[0])
	} else {
		upgrades = upgradetest.GetConfig(fork)
	}
	upgrades.ApricotPhase4MinPChainHeight = minPChainHeight

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgrades,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validatorstest.State{
		T: t,
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return snowmantest.GenesisHeight, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return defaultPChainHeight, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			var (
				thisNode = proVM.ctx.NodeID
				nodeID1  = ids.BuildTestNodeID([]byte{1})
				nodeID2  = ids.BuildTestNodeID([]byte{2})
				nodeID3  = ids.BuildTestNodeID([]byte{3})
			)
			return map[ids.NodeID]*validators.GetValidatorOutput{
				thisNode: {
					NodeID: thisNode,
					Weight: 10,
				},
				nodeID1: {
					NodeID: nodeID1,
					Weight: 5,
				},
				nodeID2: {
					NodeID: nodeID2,
					Weight: 6,
				},
				nodeID3: {
					NodeID: nodeID3,
					Weight: 7,
				},
			}, nil
		},
	}

	ctx := snowtest.Context(t, ids.ID{1})
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	db := prefixdb.New([]byte{0}, memdb.New())

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))

	proVM.Set(snowmantest.GenesisTimestamp)

	return coreVM, valState, proVM, db
}

func (vm *VM) waitForProposerWindow() error {
	ctx := context.Background()
	preferred, err := vm.getBlock(ctx, vm.preferred)
	if err != nil {
		return fmt.Errorf("couldn't get preferred block: %w", err)
	}

	pChainHeight, err := preferred.pChainHeight(ctx)
	if err != nil {
		return fmt.Errorf("couldn't get P-Chain height from tip: %w", err)
	}

	var (
		childBlockHeight = preferred.Height() + 1
		parentTimestamp  = preferred.Timestamp()
	)
	for {
		now := vm.Clock.Time().Truncate(time.Second)
		slot := proposer.TimeToSlot(parentTimestamp, now)
		delay, err := vm.MinDelayForProposer(
			ctx,
			childBlockHeight,
			pChainHeight,
			vm.ctx.NodeID,
			slot,
		)
		if err != nil {
			return err
		}

		delayUntil := parentTimestamp.Add(delay)
		if !now.Before(delayUntil) {
			return nil
		}

		vm.Clock.Set(delayUntil)
	}
}

// VM.BuildBlock tests section

func TestBuildBlockTimestampAreRoundedToSeconds(t *testing.T) {
	require := require.New(t)

	// given the same core block, BuildBlock returns the same proposer block
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	skewedTimestamp := time.Now().Truncate(time.Second).Add(time.Millisecond)
	proVM.Set(skewedTimestamp)

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.Equal(builtBlk.Timestamp().Truncate(time.Second), builtBlk.Timestamp())
}

func TestBuildBlockIsIdempotent(t *testing.T) {
	require := require.New(t)

	// given the same core block, BuildBlock returns the same proposer block
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// Mock the clock time to make sure that block timestamps will be equal
	proVM.Clock.Set(time.Now())

	builtBlk1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	builtBlk2, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.Equal(builtBlk1.Bytes(), builtBlk2.Bytes())
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	require := require.New(t)

	// setup
	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	snowBlock, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// checks
	require.IsType(&postForkBlock{}, snowBlock)
	proBlock := snowBlock.(*postForkBlock)

	require.Equal(coreBlk, proBlock.innerBlk)
}

// both core blocks and pro blocks must be built on preferred
func TestProposerBlocksAreBuiltOnPreferredProBlock(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// add two proBlks...
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	coreBlk2 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NotEqual(proBlk2.ID(), proBlk1.ID())
	require.NoError(proBlk2.Verify(t.Context()))

	// ...and set one as preferred
	var prefcoreBlk *snowmantest.Block
	coreVM.SetPreferenceF = func(_ context.Context, prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			prefcoreBlk = coreBlk1
			return nil
		case coreBlk2.ID():
			prefcoreBlk = coreBlk2
			return nil
		default:
			require.FailNow("prefID does not match coreBlk1 or coreBlk2")
			return nil
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		default:
			require.FailNow("bytes do not match coreBlk1 or coreBlk2")
			return nil, nil
		}
	}

	require.NoError(proVM.SetPreference(t.Context(), proBlk2.ID()))
	require.NoError(proVM.waitForProposerWindow())

	// build block...
	coreBlk3 := snowmantest.BuildChild(prefcoreBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	// ...show that parent is the preferred one
	require.Equal(proBlk2.ID(), builtBlk.Parent())
}

func TestCoreBlocksMustBeBuiltOnPreferredCoreBlock(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	coreBlk2 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NotEqual(proBlk1.ID(), proBlk2.ID())

	require.NoError(proBlk2.Verify(t.Context()))

	// ...and set one as preferred
	var wronglyPreferredcoreBlk *snowmantest.Block
	coreVM.SetPreferenceF = func(_ context.Context, prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			wronglyPreferredcoreBlk = coreBlk2
			return nil
		case coreBlk2.ID():
			wronglyPreferredcoreBlk = coreBlk1
			return nil
		default:
			require.FailNow("Unknown core Blocks set as preferred")
			return nil
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		default:
			require.FailNow("Wrong bytes")
			return nil, nil
		}
	}

	require.NoError(proVM.SetPreference(t.Context(), proBlk2.ID()))
	require.NoError(proVM.waitForProposerWindow())

	// build block...
	coreBlk3 := snowmantest.BuildChild(wronglyPreferredcoreBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	blk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	err = blk.Verify(t.Context())
	require.ErrorIs(err, errInnerParentMismatch)
}

// VM.ParseBlock tests section
func TestCoreBlockFailureCauseProposerBlockParseFailure(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return nil, errMarshallingFailed
	}

	innerBlk := snowmantest.BuildChild(snowmantest.Genesis)
	slb, err := statelessblock.Build(
		proVM.preferred,
		proVM.Time(),
		100, // pChainHeight,
		statelessblock.Epoch{},
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

	// test
	_, err = proVM.ParseBlock(t.Context(), proBlk.Bytes())
	require.ErrorIs(err, errMarshallingFailed)
}

func TestTwoProBlocksWrappingSameCoreBlockCanBeParsed(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// create two Proposer blocks at the same height
	innerBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	blkTimestamp := proVM.Time()

	slb1, err := statelessblock.Build(
		proVM.preferred,
		blkTimestamp,
		100, // pChainHeight,
		statelessblock.Epoch{},
		proVM.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk1 := postForkBlock{
		SignedBlock: slb1,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
		},
	}

	slb2, err := statelessblock.Build(
		proVM.preferred,
		blkTimestamp,
		200, // pChainHeight,
		statelessblock.Epoch{},
		proVM.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk2 := postForkBlock{
		SignedBlock: slb2,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
		},
	}

	require.NotEqual(proBlk1.ID(), proBlk2.ID())

	// Show that both can be parsed and retrieved
	parsedBlk1, err := proVM.ParseBlock(t.Context(), proBlk1.Bytes())
	require.NoError(err)
	parsedBlk2, err := proVM.ParseBlock(t.Context(), proBlk2.Bytes())
	require.NoError(err)

	require.Equal(proBlk1.ID(), parsedBlk1.ID())
	require.Equal(proBlk2.ID(), parsedBlk2.ID())
}

// VM.BuildBlock and VM.ParseBlock interoperability tests section
func TestTwoProBlocksWithSameParentCanBothVerify(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// one block is built from this proVM
	localcoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return localcoreBlk, nil
	}

	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(builtBlk.Verify(t.Context()))

	// another block with same parent comes from network and is parsed
	netcoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, localcoreBlk.Bytes()):
			return localcoreBlk, nil
		case bytes.Equal(b, netcoreBlk.Bytes()):
			return netcoreBlk, nil
		default:
			require.FailNow("Unknown bytes")
			return nil, nil
		}
	}

	pChainHeight, err := proVM.ctx.ValidatorState.GetCurrentHeight(t.Context())
	require.NoError(err)

	netSlb, err := statelessblock.BuildUnsigned(
		proVM.preferred,
		proVM.Time(),
		pChainHeight,
		statelessblock.Epoch{},
		netcoreBlk.Bytes(),
	)
	require.NoError(err)
	netProBlk := postForkBlock{
		SignedBlock: netSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: netcoreBlk,
		},
	}

	// prove that also block from network verifies
	require.NoError(netProBlk.Verify(t.Context()))
}

// Pre Fork tests section
func TestPreFork_Initialize(t *testing.T) {
	require := require.New(t)

	_, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// checks
	blkID, err := proVM.LastAccepted(t.Context())
	require.NoError(err)

	rtvdBlk, err := proVM.GetBlock(t.Context(), blkID)
	require.NoError(err)

	require.IsType(&preForkBlock{}, rtvdBlk)
	require.Equal(snowmantest.GenesisBytes, rtvdBlk.Bytes())
}

func TestPreFork_BuildBlock(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk)
	require.Equal(coreBlk.ID(), builtBlk.ID())
	require.Equal(coreBlk.Bytes(), builtBlk.Bytes())

	// test
	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(t.Context(), builtBlk.ID())
	require.NoError(err)
	require.Equal(builtBlk.ID(), storedBlk.ID())
}

func TestPreFork_ParseBlock(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(coreBlk.Bytes(), b)
		return coreBlk, nil
	}

	parsedBlk, err := proVM.ParseBlock(t.Context(), coreBlk.Bytes())
	require.NoError(err)
	require.IsType(&preForkBlock{}, parsedBlk)
	require.Equal(coreBlk.ID(), parsedBlk.ID())
	require.Equal(coreBlk.Bytes(), parsedBlk.Bytes())

	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(coreBlk.ID(), id)
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(t.Context(), parsedBlk.ID())
	require.NoError(err)
	require.Equal(parsedBlk.ID(), storedBlk.ID())
}

func TestPreFork_SetPreference(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.NoUpgrades, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk0 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk0, nil
	}
	builtBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	require.NoError(proVM.SetPreference(t.Context(), builtBlk.ID()))

	coreBlk1 := snowmantest.BuildChild(coreBlk0)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	nextBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.Equal(builtBlk.ID(), nextBlk.Parent())
}

// TestPostFork_SetPreference tests the SetPreference functionality after the fork
// when SetPreferenceWithContext may be called based on various conditions.
func TestPostFork_SetPreference(t *testing.T) {
	// Helper to create a block with the given epoch, P-Chain height, and optional custom timestamp
	createBlockWithEpoch := func(proVM *VM, epoch statelessblock.Epoch, blockPChainHeight uint64, customTimestamp ...time.Time) PostForkBlock {
		coreBlk := snowmantest.BuildChild(snowmantest.Genesis)

		timestamp := coreBlk.Timestamp()
		if len(customTimestamp) > 0 {
			timestamp = customTimestamp[0]
		}

		statelessBlk, err := statelessblock.BuildUnsigned(
			snowmantest.GenesisID,
			timestamp,
			blockPChainHeight,
			epoch,
			coreBlk.Bytes(),
		)
		require.NoError(t, err)

		return &postForkBlock{
			SignedBlock: statelessBlk,
			postForkCommonComponents: postForkCommonComponents{
				vm:       proVM,
				innerBlk: coreBlk,
			},
		}
	}

	testErr := errors.New("test err")

	tests := []struct {
		name                           string
		hasSetPreferenceWithContext    bool
		epochPChainHeight              *uint64 // nil = no epoch, otherwise epoch with this P-Chain height
		sealEpoch                      bool    // whether block timestamp should seal the epoch
		expectSetPreferenceWithContext bool
		expectedError                  error
	}{
		{
			name:                           "setPreferenceVM is nil - should call regular SetPreference",
			hasSetPreferenceWithContext:    false,
			epochPChainHeight:              &defaultPChainHeight,
			expectSetPreferenceWithContext: false,
		},
		{
			name:                           "preferredEpoch is empty - should call regular SetPreference",
			hasSetPreferenceWithContext:    true,
			epochPChainHeight:              nil, // no epoch
			expectSetPreferenceWithContext: false,
		},
		{
			name:                           "both conditions met - should call SetPreferenceWithContext",
			hasSetPreferenceWithContext:    true,
			epochPChainHeight:              &defaultPChainHeight,
			expectSetPreferenceWithContext: true,
		},
		{
			name:                           "SetPreferenceWithContext returns error",
			hasSetPreferenceWithContext:    true,
			epochPChainHeight:              &defaultPChainHeight,
			expectSetPreferenceWithContext: true,
			expectedError:                  testErr,
		},
		{
			name:                        "epoch sealed - next epoch has different PChainHeight",
			hasSetPreferenceWithContext: true,
			epochPChainHeight: func() *uint64 {
				h := defaultPChainHeight - 100 // older epoch height
				return &h
			}(),
			sealEpoch:                      true,
			expectSetPreferenceWithContext: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, defaultPChainHeight)
			defer func() {
				require.NoError(proVM.Shutdown(t.Context()))
			}()

			if test.hasSetPreferenceWithContext {
				coreVM.SetPreferenceWithContextF = func(_ context.Context, _ ids.ID, blockContext *block.Context) error {
					require.Equal(defaultPChainHeight, blockContext.PChainHeight)
					return test.expectedError
				}
			} else {
				proVM.setPreferenceVM = nil
				coreVM.SetPreferenceF = func(context.Context, ids.ID) error {
					return test.expectedError
				}
			}

			if test.expectSetPreferenceWithContext {
				coreVM.CantSetPreference = true
				coreVM.SetPreferenceF = nil
			} else {
				coreVM.CantSetPreferenceWithContext = true
				coreVM.SetPreferenceWithContextF = nil
			}

			// Create block based on test requirements
			var epoch statelessblock.Epoch
			if test.epochPChainHeight != nil {
				epoch = statelessblock.Epoch{
					PChainHeight: *test.epochPChainHeight,
					Number:       1,
					StartTime:    snowmantest.GenesisTimestamp.Unix(),
				}
			}

			var postForkBlk PostForkBlock
			if test.sealEpoch {
				// Create a block timestamp that seals the epoch
				epochSealingTimestamp := snowmantest.GenesisTimestamp.Add(upgrade.Default.GraniteEpochDuration)
				postForkBlk = createBlockWithEpoch(proVM, epoch, defaultPChainHeight, epochSealingTimestamp)
			} else {
				postForkBlk = createBlockWithEpoch(proVM, epoch, defaultPChainHeight)
			}

			proVM.verifiedBlocks[postForkBlk.ID()] = postForkBlk
			err := proVM.SetPreference(t.Context(), postForkBlk.ID())
			require.ErrorIs(err, test.expectedError)
		})
	}
}

func TestExpiredBuildBlock(t *testing.T) {
	require := require.New(t)

	coreVM := &blocktest.VM{}
	coreVM.T = t

	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	events := make(chan common.Message, 1)
	coreVM.WaitForEventF = func(ctx context.Context) (common.Message, error) {
		select {
		case <-ctx.Done():
			return 0, nil
		case event := <-events:
			return event, nil
		}
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validatorstest.State{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return snowmantest.GenesisHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		nodeID := ids.BuildTestNodeID([]byte{1})
		return map[ids.NodeID]*validators.GetValidatorOutput{
			nodeID: {
				NodeID: nodeID,
				Weight: 100,
			},
		}, nil
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	coreVM.InitializeF = func(
		_ context.Context,
		_ *snow.Context,
		_ database.Database,
		_ []byte,
		_ []byte,
		_ []byte,
		_ []*common.Fx,
		_ common.AppSender,
	) error {
		return nil
	}

	// make sure that DBs are compressed correctly
	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		memdb.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))

	// Notify the proposer VM of a new block on the inner block side
	events <- common.PendingTxs
	// The first notification will be read from the consensus engine
	msg, err := proVM.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)

	// Before calling BuildBlock, verify a remote block and set it as the
	// preferred block.
	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	statelessBlock, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		0,
		statelessblock.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

	proVM.Clock.Set(statelessBlock.Timestamp())

	parsedBlock, err := proVM.ParseBlock(t.Context(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parsedBlock.ID()))

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		require.FailNow(fmt.Errorf("%w: BuildBlock", errUnexpectedCall).Error())
		return nil, errUnexpectedCall
	}

	// Because we are now building on a different block, the proposer window
	// shouldn't have started.
	_, err = proVM.BuildBlock(t.Context())
	require.ErrorIs(err, errProposerWindowNotStarted)
}

type wrappedBlock struct {
	snowman.Block
	verified bool
}

func (b *wrappedBlock) Accept(ctx context.Context) error {
	if !b.verified {
		return errUnverifiedBlock
	}
	return b.Block.Accept(ctx)
}

func (b *wrappedBlock) Verify(ctx context.Context) error {
	if err := b.Block.Verify(ctx); err != nil {
		return err
	}
	b.verified = true
	return nil
}

func TestInnerBlockDeduplication(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk0 := &wrappedBlock{
		Block: coreBlk,
	}
	coreBlk1 := &wrappedBlock{
		Block: coreBlk,
	}
	statelessBlock0, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		0,
		statelessblock.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)
	statelessBlock1, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		1,
		statelessblock.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock0, err := proVM.ParseBlock(t.Context(), statelessBlock0.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock0.Verify(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), parsedBlock0.ID()))

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock1, err := proVM.ParseBlock(t.Context(), statelessBlock1.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock1.Verify(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), parsedBlock1.ID()))

	require.NoError(parsedBlock1.Accept(t.Context()))
}

func TestInnerVMRollback(t *testing.T) {
	require := require.New(t)

	valState := &validatorstest.State{
		T: t,
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return defaultPChainHeight, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			nodeID := ids.BuildTestNodeID([]byte{1})
			return map[ids.NodeID]*validators.GetValidatorOutput{
				nodeID: {
					NodeID: nodeID,
					Weight: 100,
				},
			}, nil
		},
	}

	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(
				context.Context,
				*snow.Context,
				database.Database,
				[]byte,
				[]byte,
				[]byte,
				[]*common.Fx,
				common.AppSender,
			) error {
				return nil
			},
		},
		ParseBlockF: func(_ context.Context, b []byte) (snowman.Block, error) {
			switch {
			case bytes.Equal(b, snowmantest.GenesisBytes):
				return snowmantest.Genesis, nil
			default:
				return nil, errUnknownBlock
			}
		},
		GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			switch blkID {
			case snowmantest.GenesisID:
				return snowmantest.Genesis, nil
			default:
				return nil, errUnknownBlock
			}
		},
		LastAcceptedF: snowmantest.MakeLastAcceptedBlockF(
			[]*snowmantest.Block{snowmantest.Genesis},
		),
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	db := memdb.New()

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	statelessBlock, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		0,
		statelessblock.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

	proVM.Clock.Set(statelessBlock.Timestamp())

	lastAcceptedID, err := proVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(snowmantest.GenesisID, lastAcceptedID)

	parsedBlock, err := proVM.ParseBlock(t.Context(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), parsedBlock.ID()))
	require.NoError(parsedBlock.Accept(t.Context()))

	lastAcceptedID, err = proVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(parsedBlock.ID(), lastAcceptedID)

	// Restart the node and have the inner VM rollback state.
	require.NoError(proVM.Shutdown(t.Context()))
	coreBlk.Status = snowtest.Undecided

	proVM = New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	lastAcceptedID, err = proVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(snowmantest.GenesisID, lastAcceptedID)
}

func TestBuildBlockDuringWindow(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
		}, nil
	}

	coreBlk0 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1 := snowmantest.BuildChild(coreBlk0)
	statelessBlock0, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		0,
		statelessblock.Epoch{},
		coreBlk0.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM.Clock.Set(statelessBlock0.Timestamp())

	statefulBlock0, err := proVM.ParseBlock(t.Context(), statelessBlock0.Bytes())
	require.NoError(err)

	require.NoError(statefulBlock0.Verify(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), statefulBlock0.ID()))

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}

	statefulBlock1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(statefulBlock1.Verify(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), statefulBlock1.ID()))

	require.NoError(statefulBlock0.Accept(t.Context()))

	require.NoError(statefulBlock1.Accept(t.Context()))
}

// Ensure that Accepting a PostForkBlock (A) containing core block (X) causes
// core block (Y) and (Z) to also be rejected.
//
//	     G
//	   /   \
//	A(X)   B(Y)
//	        |
//	       C(Z)
func TestTwoForks_OneIsAccepted(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// create pre-fork block X and post-fork block A
	xBlock := snowmantest.BuildChild(snowmantest.Genesis)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	coreVM.BuildBlockF = nil
	require.NoError(aBlock.Verify(t.Context()))

	// use a different way to construct pre-fork block Y and post-fork block B
	yBlock := snowmantest.BuildChild(snowmantest.Genesis)

	ySlb, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		defaultPChainHeight,
		statelessblock.Epoch{},
		yBlock.Bytes(),
	)
	require.NoError(err)

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
		},
	}

	require.NoError(bBlock.Verify(t.Context()))

	// append Z/C to Y/B
	zBlock := snowmantest.BuildChild(yBlock)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return zBlock, nil
	}
	require.NoError(proVM.SetPreference(t.Context(), bBlock.ID()))
	proVM.Set(proVM.Time().Add(proposer.MaxBuildDelay))
	cBlock, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	coreVM.BuildBlockF = nil

	require.NoError(cBlock.Verify(t.Context()))

	require.Equal(bBlock.Parent(), aBlock.Parent())
	require.Equal(yBlock.ID(), zBlock.Parent())
	require.Equal(bBlock.ID(), cBlock.Parent())

	require.NotEqual(snowtest.Rejected, yBlock.Status)

	// accept A
	require.NoError(aBlock.Accept(t.Context()))

	require.Equal(snowtest.Accepted, xBlock.Status)
	require.Equal(snowtest.Rejected, yBlock.Status)
	require.Equal(snowtest.Rejected, zBlock.Status)
}

func TestTooFarAdvanced(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	xBlock := snowmantest.BuildChild(snowmantest.Genesis)
	yBlock := snowmantest.BuildChild(xBlock)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(aBlock.Verify(t.Context()))

	ySlb, err := statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(maxSkew),
		defaultPChainHeight,
		statelessblock.Epoch{},
		yBlock.Bytes(),
	)
	require.NoError(err)

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
		},
	}

	err = bBlock.Verify(t.Context())
	require.ErrorIs(err, errProposerWindowNotStarted)

	ySlb, err = statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(proposer.MaxVerifyDelay),
		defaultPChainHeight,
		statelessblock.Epoch{},
		yBlock.Bytes(),
	)

	require.NoError(err)

	bBlock = postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
		},
	}

	err = bBlock.Verify(t.Context())
	require.ErrorIs(err, errTimeTooAdvanced)
}

// Ensure that Accepting a PostForkOption (B) causes both the other option and
// the core block in the other option to be rejected.
//
//	    G
//	    |
//	   A(X)
//	  /    \
//	B(Y)   C(Z)
//
// Y is X.opts[0]
// Z is X.opts[1]
func TestTwoOptions_OneIsAccepted(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	xTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	xBlock := &TestOptionsBlock{
		Block: *xTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(xTestBlock),
			snowmantest.BuildChild(xTestBlock),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlockIntf, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)

	opts, err := aBlock.Options(t.Context())
	require.NoError(err)

	bBlock := opts[0]
	cBlock := opts[1]

	require.NoError(aBlock.Verify(t.Context()))
	require.NoError(bBlock.Verify(t.Context()))
	require.NoError(cBlock.Verify(t.Context()))

	require.NoError(aBlock.Accept(t.Context()))
	require.NoError(bBlock.Accept(t.Context()))

	// the other pre-fork option should be rejected
	require.Equal(snowtest.Rejected, xBlock.opts[1].Status)
}

// Ensure that given the chance, built blocks will reference a lagged P-chain
// height.
func TestLaggedPChainHeight(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	innerBlock := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock, nil
	}
	blockIntf, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&postForkBlock{}, blockIntf)
	block := blockIntf.(*postForkBlock)

	pChainHeight := block.PChainHeight()
	require.Equal(snowmantest.GenesisHeight, pChainHeight)
}

// Ensure that rejecting a block does not modify the accepted block ID for the
// rejected height.
func TestRejectedHeightNotIndexed(t *testing.T) {
	require := require.New(t)

	coreHeights := []ids.ID{snowmantest.GenesisID}

	initialState := []byte("genesis state")
	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
		GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
			if height >= uint64(len(coreHeights)) {
				return ids.Empty, errTooHigh
			}
			return coreHeights[height], nil
		},
	}

	coreVM.InitializeF = func(context.Context, *snow.Context, database.Database,
		[]byte, []byte, []byte,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.Latest, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validatorstest.State{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return snowmantest.GenesisHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			thisNode = proVM.ctx.NodeID
			nodeID1  = ids.BuildTestNodeID([]byte{1})
			nodeID2  = ids.BuildTestNodeID([]byte{2})
			nodeID3  = ids.BuildTestNodeID([]byte{3})
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			thisNode: {
				NodeID: thisNode,
				Weight: 10,
			},
			nodeID1: {
				NodeID: nodeID1,
				Weight: 5,
			},
			nodeID2: {
				NodeID: nodeID2,
				Weight: 6,
			},
			nodeID3: {
				NodeID: nodeID3,
				Weight: 7,
			},
		}, nil
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))

	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))

	// create inner block X and outer block A
	xBlock := snowmantest.BuildChild(snowmantest.Genesis)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	coreVM.BuildBlockF = nil
	require.NoError(aBlock.Verify(t.Context()))

	// use a different way to construct inner block Y and outer block B
	yBlock := snowmantest.BuildChild(snowmantest.Genesis)

	ySlb, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		defaultPChainHeight,
		statelessblock.Epoch{},
		yBlock.Bytes(),
	)
	require.NoError(err)

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
		},
	}

	require.NoError(bBlock.Verify(t.Context()))

	// accept A
	require.NoError(aBlock.Accept(t.Context()))
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(t.Context(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// reject B
	require.NoError(bBlock.Reject(t.Context()))

	blkID, err = proVM.GetBlockIDAtHeight(t.Context(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)
}

// Ensure that rejecting an option block does not modify the accepted block ID
// for the rejected height.
func TestRejectedOptionHeightNotIndexed(t *testing.T) {
	require := require.New(t)

	coreHeights := []ids.ID{snowmantest.GenesisID}

	initialState := []byte("genesis state")
	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
		},
		GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
			if height >= uint64(len(coreHeights)) {
				return ids.Empty, errTooHigh
			}
			return coreHeights[height], nil
		},
	}

	coreVM.InitializeF = func(context.Context, *snow.Context, database.Database,
		[]byte, []byte, []byte,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.Latest, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validatorstest.State{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return snowmantest.GenesisHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			thisNode = proVM.ctx.NodeID
			nodeID1  = ids.BuildTestNodeID([]byte{1})
			nodeID2  = ids.BuildTestNodeID([]byte{2})
			nodeID3  = ids.BuildTestNodeID([]byte{3})
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			thisNode: {
				NodeID: thisNode,
				Weight: 10,
			},
			nodeID1: {
				NodeID: nodeID1,
				Weight: 5,
			},
			nodeID2: {
				NodeID: nodeID2,
				Weight: 6,
			},
			nodeID3: {
				NodeID: nodeID3,
				Weight: 7,
			},
		}, nil
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))

	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))

	xTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	xBlock := &TestOptionsBlock{
		Block: *xTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(xTestBlock),
			snowmantest.BuildChild(xTestBlock),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlockIntf, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)

	opts, err := aBlock.Options(t.Context())
	require.NoError(err)

	require.NoError(aBlock.Verify(t.Context()))

	bBlock := opts[0]
	require.NoError(bBlock.Verify(t.Context()))

	cBlock := opts[1]
	require.NoError(cBlock.Verify(t.Context()))

	// accept A
	require.NoError(aBlock.Accept(t.Context()))
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(t.Context(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// accept B
	require.NoError(bBlock.Accept(t.Context()))
	coreHeights = append(coreHeights, xBlock.opts[0].ID())

	blkID, err = proVM.GetBlockIDAtHeight(t.Context(), bBlock.Height())
	require.NoError(err)
	require.Equal(bBlock.ID(), blkID)

	// reject C
	require.NoError(cBlock.Reject(t.Context()))

	blkID, err = proVM.GetBlockIDAtHeight(t.Context(), cBlock.Height())
	require.NoError(err)
	require.Equal(bBlock.ID(), blkID)
}

func TestVMInnerBlkCache(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create a VM
	innerVM := blockmock.NewChainVM(ctrl)
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

	innerVM.EXPECT().WaitForEvent(gomock.Any()).Return(common.PendingTxs, nil).AnyTimes()

	innerVM.EXPECT().Initialize(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil)
	innerVM.EXPECT().Shutdown(gomock.Any()).Return(nil)

	{
		innerBlk := snowmanmock.NewBlock(ctrl)
		innerBlkID := ids.GenerateTestID()
		innerVM.EXPECT().LastAccepted(gomock.Any()).Return(innerBlkID, nil)
		innerVM.EXPECT().GetBlock(gomock.Any(), innerBlkID).Return(innerBlk, nil)
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	// Create a block near the tip (0).
	blkNearTipInnerBytes := []byte{1}
	blkNearTip, err := statelessblock.Build(
		ids.GenerateTestID(),   // parent
		time.Time{},            // timestamp
		1,                      // pChainHeight
		statelessblock.Epoch{}, // epoch
		vm.StakingCertLeaf,     // cert
		blkNearTipInnerBytes,   // inner blk bytes
		vm.ctx.ChainID,         // chain ID
		vm.StakingLeafSigner,   // key
	)
	require.NoError(err)

	// We will ask the inner VM to parse.
	mockInnerBlkNearTip := snowmanmock.NewBlock(ctrl)
	mockInnerBlkNearTip.EXPECT().Height().Return(uint64(1)).Times(2)
	mockInnerBlkNearTip.EXPECT().Bytes().Return(blkNearTipInnerBytes).Times(1)

	innerVM.EXPECT().ParseBlock(gomock.Any(), blkNearTipInnerBytes).Return(mockInnerBlkNearTip, nil).Times(2)
	_, err = vm.ParseBlock(t.Context(), blkNearTip.Bytes())
	require.NoError(err)

	// Block should now be in cache because it's a post-fork block
	// and close to the tip.
	gotBlk, ok := vm.innerBlkCache.Get(blkNearTip.ID())
	require.True(ok)
	require.Equal(mockInnerBlkNearTip, gotBlk)
	require.Zero(vm.lastAcceptedHeight)

	// Clear the cache
	vm.innerBlkCache.Flush()

	// Advance the tip height
	vm.lastAcceptedHeight = innerBlkCacheSize + 1

	// Parse the block again. This time it shouldn't be cached
	// because it's not close to the tip.
	_, err = vm.ParseBlock(t.Context(), blkNearTip.Bytes())
	require.NoError(err)

	_, ok = vm.innerBlkCache.Get(blkNearTip.ID())
	require.False(ok)
}

type blockWithVerifyContext struct {
	*snowmanmock.Block
	*blockmock.WithVerifyContext
}

// Ensures that we call [VerifyWithContext] rather than [Verify] on blocks that
// implement [block.WithVerifyContext] and that returns true for
// [ShouldVerifyWithContext].
func TestVM_VerifyBlockWithContext(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Create a VM
	innerVM := blockmock.NewChainVM(ctrl)
	innerVM.EXPECT().WaitForEvent(gomock.Any()).Return(common.PendingTxs, nil).AnyTimes()

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

	// make sure that DBs are compressed correctly
	db := prefixdb.New([]byte{}, memdb.New())

	innerVM.EXPECT().Initialize(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil)
	innerVM.EXPECT().Shutdown(gomock.Any()).Return(nil)

	{
		innerBlk := snowmanmock.NewBlock(ctrl)
		innerBlkID := ids.GenerateTestID()
		innerVM.EXPECT().LastAccepted(gomock.Any()).Return(innerBlkID, nil)
		innerVM.EXPECT().GetBlock(gomock.Any(), innerBlkID).Return(innerBlk, nil)
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NodeID = ids.NodeIDFromCert(pTestCert)

	require.NoError(vm.Initialize(
		t.Context(),
		snowCtx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	{
		pChainHeight := uint64(0)
		innerBlk := blockWithVerifyContext{
			Block:             snowmanmock.NewBlock(ctrl),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		innerBlk.WithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(2)
		innerBlk.WithVerifyContext.EXPECT().VerifyWithContext(t.Context(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
		).Return(nil)
		innerBlk.Block.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.Block.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.Block.EXPECT().Bytes().Return(utils.RandomBytes(1024)).AnyTimes()

		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()

		require.NoError(vm.verifyAndRecordInnerBlk(
			t.Context(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
			blk,
		))

		// Call VerifyWithContext again but with a different P-Chain height
		blk.EXPECT().setInnerBlk(innerBlk).AnyTimes()
		pChainHeight++
		innerBlk.WithVerifyContext.EXPECT().VerifyWithContext(t.Context(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
		).Return(nil)

		require.NoError(vm.verifyAndRecordInnerBlk(
			t.Context(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
			blk,
		))
	}

	{
		// Ensure we call Verify on a block that returns
		// false for ShouldVerifyWithContext
		innerBlk := blockWithVerifyContext{
			Block:             snowmanmock.NewBlock(ctrl),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		innerBlk.WithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(false, nil)
		innerBlk.Block.EXPECT().Verify(gomock.Any()).Return(nil)
		innerBlk.Block.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.Block.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()
		require.NoError(vm.verifyAndRecordInnerBlk(
			t.Context(),
			&block.Context{
				PChainHeight: 1,
			},
			blk,
		))
	}

	{
		// Ensure we call Verify on a block that doesn't have a valid context
		innerBlk := blockWithVerifyContext{
			Block:             snowmanmock.NewBlock(ctrl),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		innerBlk.Block.EXPECT().Verify(gomock.Any()).Return(nil)
		innerBlk.Block.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.Block.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()
		require.NoError(vm.verifyAndRecordInnerBlk(t.Context(), nil, blk))
	}
}

func TestHistoricalBlockDeletion(t *testing.T) {
	require := require.New(t)

	acceptedBlocks := []*snowmantest.Block{snowmantest.Genesis}
	currentHeight := uint64(0)

	initialState := []byte("genesis state")
	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, []*common.Fx, common.AppSender) error {
				return nil
			},
		},
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			return acceptedBlocks[currentHeight].ID(), nil
		},
		GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			for _, blk := range acceptedBlocks {
				if blkID == blk.ID() {
					return blk, nil
				}
			}
			return nil, errUnknownBlock
		},
		ParseBlockF: func(_ context.Context, b []byte) (snowman.Block, error) {
			for _, blk := range acceptedBlocks {
				if bytes.Equal(b, blk.Bytes()) {
					return blk, nil
				}
			}
			return nil, errUnknownBlock
		},
		GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
			if height >= uint64(len(acceptedBlocks)) {
				return ids.Empty, errTooHigh
			}
			return acceptedBlocks[height].ID(), nil
		},
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = &validatorstest.State{
		T: t,
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return snowmantest.GenesisHeight, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return defaultPChainHeight, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return nil, nil
		},
	}

	// make sure that DBs are compressed correctly
	db := prefixdb.New([]byte{}, memdb.New())

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	lastAcceptedID, err := proVM.LastAccepted(t.Context())
	require.NoError(err)

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), lastAcceptedID))

	issueBlock := func() {
		lastAcceptedBlock := acceptedBlocks[currentHeight]
		innerBlock := snowmantest.BuildChild(lastAcceptedBlock)

		coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
			return innerBlock, nil
		}
		proBlock, err := proVM.BuildBlock(t.Context())
		require.NoError(err)

		require.NoError(proBlock.Verify(t.Context()))
		require.NoError(proVM.SetPreference(t.Context(), proBlock.ID()))
		require.NoError(proBlock.Accept(t.Context()))

		acceptedBlocks = append(acceptedBlocks, innerBlock)
		currentHeight++
	}

	requireHeights := func(start, end uint64) {
		for i := start; i <= end; i++ {
			_, err := proVM.GetBlockIDAtHeight(t.Context(), i)
			require.NoError(err)
		}
	}

	requireMissingHeights := func(start, end uint64) {
		for i := start; i <= end; i++ {
			_, err := proVM.GetBlockIDAtHeight(t.Context(), i)
			require.ErrorIs(err, database.ErrNotFound)
		}
	}

	requireNumHeights := func(numIndexed uint64) {
		requireHeights(0, 0)
		requireMissingHeights(1, currentHeight-numIndexed-1)
		requireHeights(currentHeight-numIndexed, currentHeight)
	}

	// Because block pruning is disabled by default, the heights should be
	// populated for every accepted block.
	requireHeights(0, currentHeight)

	issueBlock()
	requireHeights(0, currentHeight)

	issueBlock()
	requireHeights(0, currentHeight)

	issueBlock()
	requireHeights(0, currentHeight)

	issueBlock()
	requireHeights(0, currentHeight)

	issueBlock()
	requireHeights(0, currentHeight)

	require.NoError(proVM.Shutdown(t.Context()))

	numHistoricalBlocks := uint64(2)
	proVM = New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	lastAcceptedID, err = proVM.LastAccepted(t.Context())
	require.NoError(err)

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), lastAcceptedID))

	// Verify that old blocks were pruned during startup
	requireNumHeights(numHistoricalBlocks)

	// As we issue new blocks, the oldest indexed height should be pruned.
	issueBlock()
	requireNumHeights(numHistoricalBlocks)

	issueBlock()
	requireNumHeights(numHistoricalBlocks)

	require.NoError(proVM.Shutdown(t.Context()))

	newNumHistoricalBlocks := numHistoricalBlocks + 2
	proVM = New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: newNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	lastAcceptedID, err = proVM.LastAccepted(t.Context())
	require.NoError(err)

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), lastAcceptedID))

	// The height index shouldn't be modified at this point
	requireNumHeights(numHistoricalBlocks)

	// As we issue new blocks, the number of indexed blocks should increase
	// until we hit our target again.
	issueBlock()
	requireNumHeights(numHistoricalBlocks + 1)

	issueBlock()
	requireNumHeights(newNumHistoricalBlocks)

	issueBlock()
	requireNumHeights(newNumHistoricalBlocks)
}

func TestGetPostDurangoSlotTimeWithNoValidators(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		// If there are no validators, anyone should be able to propose a block.
		return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
	}

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	statelessBlock, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		0,
		statelessblock.Epoch{},
		coreBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

	statefulBlock, err := proVM.ParseBlock(t.Context(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(statefulBlock.Verify(t.Context()))

	currentTime := proVM.Clock.Time().Truncate(time.Second)
	parentTimestamp := statefulBlock.Timestamp()
	slotTime, err := proVM.getPostDurangoSlotTime(
		t.Context(),
		statefulBlock.Height()+1,
		statelessBlock.PChainHeight(),
		proposer.TimeToSlot(parentTimestamp, currentTime),
		parentTimestamp,
	)
	require.NoError(err)
	require.Equal(parentTimestamp.Add(proVM.MinBlkDelay), slotTime)
}

func TestLocalParse(t *testing.T) {
	innerVM := &blocktest.VM{
		ParseBlockF: func(_ context.Context, rawBlock []byte) (snowman.Block, error) {
			return &snowmantest.Block{BytesV: rawBlock}, nil
		},
	}

	innerVM.VM.WaitForEventF = func(_ context.Context) (common.Message, error) {
		return common.PendingTxs, nil
	}

	chainID := ids.GenerateTestID()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := statelessblock.Build(
		ids.ID{1},
		time.Unix(123, 0),
		uint64(42),
		statelessblock.Epoch{},
		cert,
		[]byte{1, 2, 3},
		chainID,
		key,
	)
	require.NoError(t, err)

	properlySignedBlock := signedBlock.Bytes()

	improperlySignedBlock := make([]byte, len(properlySignedBlock))
	copy(improperlySignedBlock, properlySignedBlock)
	improperlySignedBlock[len(improperlySignedBlock)-1] = ^improperlySignedBlock[len(improperlySignedBlock)-1]

	conf := Config{
		MinBlkDelay:         DefaultMinBlockDelay,
		NumHistoricalBlocks: DefaultNumHistoricalBlocks,
		StakingLeafSigner:   pTestSigner,
		StakingCertLeaf:     pTestCert,
		Registerer:          prometheus.NewRegistry(),
	}

	vm := New(innerVM, conf)
	defer func() {
		require.NoError(t, vm.Shutdown(t.Context()))
	}()

	db := prefixdb.New([]byte{}, memdb.New())

	_ = vm.Initialize(t.Context(), &snow.Context{
		Log:     logging.NoLog{},
		ChainID: chainID,
	}, db, nil, nil, nil, nil, nil)

	tests := []struct {
		name           string
		f              block.ParseFunc
		block          []byte
		resultingBlock interface{}
	}{
		{
			name:           "local parse as post-fork",
			f:              vm.ParseLocalBlock,
			block:          improperlySignedBlock,
			resultingBlock: &postForkBlock{},
		},
		{
			name:           "parse as pre-fork",
			f:              vm.ParseBlock,
			block:          improperlySignedBlock,
			resultingBlock: &preForkBlock{},
		},
		{
			name:           "parse as post-fork",
			f:              vm.ParseBlock,
			block:          properlySignedBlock,
			resultingBlock: &postForkBlock{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block, err := test.f(t.Context(), test.block)
			require.NoError(t, err)
			require.IsType(t, test.resultingBlock, block)
		})
	}
}

func TestTimestampMetrics(t *testing.T) {
	ctx := t.Context()

	coreVM, _, proVM, _ := initTestProposerVM(t, upgradetest.ApricotPhase4, 0)

	defer func() {
		require.NoError(t, proVM.Shutdown(ctx))
	}()

	innerBlock := snowmantest.BuildChild(snowmantest.Genesis)

	// The actual numbers do not matter here, we just verify the metrics are
	// populated correctly.
	outerTime := upgrade.InitiallyActiveTime.Add(1000 * time.Second)
	innerTime := upgrade.InitiallyActiveTime.Add(500 * time.Second)
	proVM.Clock.Set(outerTime)
	innerBlock.TimestampV = innerTime

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock, nil
	}
	outerBlock, err := proVM.BuildBlock(ctx)
	require.NoError(t, err)
	require.IsType(t, &postForkBlock{}, outerBlock)
	require.NoError(t, outerBlock.Accept(ctx))

	gaugeVec := proVM.lastAcceptedTimestampGaugeVec
	tests := []struct {
		blockType string
		want      time.Time
	}{
		{innerBlockTypeMetricLabel, innerTime},
		{outerBlockTypeMetricLabel, outerTime},
	}
	for _, tt := range tests {
		t.Run(tt.blockType, func(t *testing.T) {
			gauge, err := gaugeVec.GetMetricWithLabelValues(tt.blockType)
			require.NoError(t, err)
			require.Equal(t, float64(tt.want.Unix()), testutil.ToFloat64(gauge))
		})
	}
}

func TestSelectChildPChainHeight(t *testing.T) {
	beforeOverrideEnds := fujiOverridePChainHeightUntilTimestamp.Add(-time.Minute)
	for _, test := range []struct {
		name                 string
		time                 time.Time
		networkID            uint32
		subnetID             ids.ID
		currentPChainHeight  uint64
		minPChainHeight      uint64
		expectedPChainHeight uint64
	}{
		{
			name:                 "no override - mainnet",
			time:                 beforeOverrideEnds,
			networkID:            constants.MainnetID,
			subnetID:             ids.GenerateTestID(),
			currentPChainHeight:  fujiOverridePChainHeightUntilHeight + 2,
			minPChainHeight:      fujiOverridePChainHeightUntilHeight - 5,
			expectedPChainHeight: fujiOverridePChainHeightUntilHeight + 2,
		},
		{
			name:                 "no override - primary network",
			time:                 beforeOverrideEnds,
			networkID:            constants.FujiID,
			subnetID:             constants.PrimaryNetworkID,
			currentPChainHeight:  fujiOverridePChainHeightUntilHeight + 2,
			minPChainHeight:      fujiOverridePChainHeightUntilHeight - 5,
			expectedPChainHeight: fujiOverridePChainHeightUntilHeight + 2,
		},
		{
			name:                 "no override - expired network",
			time:                 fujiOverridePChainHeightUntilTimestamp,
			networkID:            constants.FujiID,
			subnetID:             ids.GenerateTestID(),
			currentPChainHeight:  fujiOverridePChainHeightUntilHeight + 2,
			minPChainHeight:      fujiOverridePChainHeightUntilHeight - 5,
			expectedPChainHeight: fujiOverridePChainHeightUntilHeight + 2,
		},
		{
			name:                 "no override - chain previously advanced",
			time:                 beforeOverrideEnds,
			networkID:            constants.FujiID,
			subnetID:             ids.GenerateTestID(),
			currentPChainHeight:  fujiOverridePChainHeightUntilHeight + 2,
			minPChainHeight:      fujiOverridePChainHeightUntilHeight + 1,
			expectedPChainHeight: fujiOverridePChainHeightUntilHeight + 2,
		},
		{
			name:                 "override",
			time:                 beforeOverrideEnds,
			networkID:            constants.FujiID,
			subnetID:             ids.GenerateTestID(),
			currentPChainHeight:  fujiOverridePChainHeightUntilHeight + 2,
			minPChainHeight:      fujiOverridePChainHeightUntilHeight - 5,
			expectedPChainHeight: fujiOverridePChainHeightUntilHeight - 5,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			_, vdrState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
			defer func() {
				require.NoError(proVM.Shutdown(t.Context()))
			}()

			proVM.Clock.Set(test.time)
			proVM.ctx.NetworkID = test.networkID
			proVM.ctx.SubnetID = test.subnetID

			vdrState.GetMinimumHeightF = func(context.Context) (uint64, error) {
				return test.currentPChainHeight, nil
			}

			actualPChainHeight, err := proVM.selectChildPChainHeight(
				t.Context(),
				test.minPChainHeight,
			)
			require.NoError(err)
			require.Equal(test.expectedPChainHeight, actualPChainHeight)
		})
	}
}

// This tests the case where a chain missed the Granite activation and continued
// producing blocks without epochs. When a new node joins the network, it needs
// to allow the delayed activation of Granite.
func TestBootstrappingWithDelayedGraniteActivation(t *testing.T) {
	require := require.New(t)

	// innerVMBlks is appended to throughout the test, which modifies the
	// behavior of coreVM.
	innerVMBlks := []*snowmantest.Block{
		snowmantest.Genesis,
	}

	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, []*common.Fx, common.AppSender) error {
				return nil
			},
		},
		ParseBlockF: func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
			for _, blk := range innerVMBlks {
				if bytes.Equal(blk.Bytes(), blkBytes) {
					return blk, nil
				}
			}
			return nil, errUnknownBlock
		},
		GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			for _, blk := range innerVMBlks {
				if blk.Status == snowtest.Accepted && blk.ID() == blkID {
					return blk, nil
				}
			}
			return nil, database.ErrNotFound
		},
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			var (
				lastAcceptedID     ids.ID
				lastAcceptedHeight uint64
			)
			for _, blk := range innerVMBlks {
				if blk.Status == snowtest.Accepted && blk.Height() >= lastAcceptedHeight {
					lastAcceptedID = blk.ID()
					lastAcceptedHeight = blk.Height()
				}
			}
			return lastAcceptedID, nil
		},
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)
	proVM.Set(snowmantest.GenesisTimestamp)

	// We mark the P-chain as having synced to height=1.
	const currentPChainHeight = 1
	valState := &validatorstest.State{
		T: t,
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return currentPChainHeight, nil
		},
	}

	ctx := snowtest.Context(t, ids.ID{1})
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		memdb.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	require.NoError(proVM.SetState(t.Context(), snow.Bootstrapping))

	// During bootstrapping, the first post-fork block is verified against the
	// P-chain height, so we provide a valid height.
	innerBlock1 := snowmantest.BuildChild(snowmantest.Genesis)
	innerVMBlks = append(innerVMBlks, innerBlock1)
	statelessBlock1, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		currentPChainHeight,
		statelessblock.Epoch{},
		innerBlock1.Bytes(),
	)
	require.NoError(err)

	block1, err := proVM.ParseBlock(t.Context(), statelessBlock1.Bytes())
	require.NoError(err)

	require.NoError(block1.Verify(t.Context()))
	require.NoError(block1.Accept(t.Context()))

	// This block should include an epoch. But since the node is still
	// bootstrapping, it should allow the block.
	innerBlock2 := snowmantest.BuildChild(innerBlock1)
	innerVMBlks = append(innerVMBlks, innerBlock2)
	statelessBlock2, err := statelessblock.Build(
		statelessBlock1.ID(),
		statelessBlock1.Timestamp(),
		currentPChainHeight,
		statelessblock.Epoch{},
		pTestCert,
		innerBlock2.Bytes(),
		ctx.ChainID,
		pTestSigner,
	)
	require.NoError(err)

	block2, err := proVM.ParseBlock(t.Context(), statelessBlock2.Bytes())
	require.NoError(err)

	require.NoError(block2.Verify(t.Context()))
	require.NoError(block2.Accept(t.Context()))
}

// This tests the case where a chain has bootstrapped to a last accepted block
// which references a P-Chain height that is not locally accepted yet.
func TestBootstrappingAheadOfPChainBuildBlockRegression(t *testing.T) {
	t.Skip("FIXME")

	require := require.New(t)

	// innerVMBlks is appended to throughout the test, which modifies the
	// behavior of coreVM.
	innerVMBlks := []*snowmantest.Block{
		snowmantest.Genesis,
	}

	coreVM := &blocktest.VM{
		VM: enginetest.VM{
			T: t,
			InitializeF: func(_ context.Context, _ *snow.Context, _ database.Database, _ []byte, _ []byte, _ []byte, _ []*common.Fx, _ common.AppSender) error {
				return nil
			},
		},
		ParseBlockF: func(_ context.Context, blkBytes []byte) (snowman.Block, error) {
			for _, blk := range innerVMBlks {
				if bytes.Equal(blk.Bytes(), blkBytes) {
					return blk, nil
				}
			}
			return nil, errUnknownBlock
		},
		GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			for _, blk := range innerVMBlks {
				if blk.Status == snowtest.Accepted && blk.ID() == blkID {
					return blk, nil
				}
			}
			return nil, database.ErrNotFound
		},
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			var (
				lastAcceptedID     ids.ID
				lastAcceptedHeight uint64
			)
			for _, blk := range innerVMBlks {
				if blk.Status == snowtest.Accepted && blk.Height() >= lastAcceptedHeight {
					lastAcceptedID = blk.ID()
					lastAcceptedHeight = blk.Height()
				}
			}
			return lastAcceptedID, nil
		},
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)
	proVM.Set(snowmantest.GenesisTimestamp)

	// We mark the P-chain as having synced to height=1.
	const currentPChainHeight = 1
	valState := &validatorstest.State{
		T: t,
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return currentPChainHeight, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return currentPChainHeight, nil
		},
		GetValidatorSetF: func(_ context.Context, height uint64, _ ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			if height > currentPChainHeight {
				return nil, fmt.Errorf("requested height (%d) > current P-chain height (%d)", height, currentPChainHeight)
			}
			return map[ids.NodeID]*validators.GetValidatorOutput{
				proVM.ctx.NodeID: {
					NodeID: proVM.ctx.NodeID,
					Weight: 10,
				},
			}, nil
		},
	}

	ctx := snowtest.Context(t, ids.ID{1})
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	db := prefixdb.New([]byte{0}, memdb.New())

	require.NoError(proVM.Initialize(
		t.Context(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	require.NoError(proVM.SetState(t.Context(), snow.Bootstrapping))

	// During bootstrapping, the first post-fork block is verified against the
	// P-chain height, so we provide a valid height.
	innerBlock1 := snowmantest.BuildChild(snowmantest.Genesis)
	innerVMBlks = append(innerVMBlks, innerBlock1)
	statelessBlock1, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		currentPChainHeight,
		statelessblock.Epoch{},
		innerBlock1.Bytes(),
	)
	require.NoError(err)

	block1, err := proVM.ParseBlock(t.Context(), statelessBlock1.Bytes())
	require.NoError(err)

	require.NoError(block1.Verify(t.Context()))
	require.NoError(block1.Accept(t.Context()))

	// During bootstrapping, the additional post-fork blocks are not verified
	// against the local P-chain height, so even if we provide a height higher
	// than our P-chain height, verification will succeed.
	innerBlock2 := snowmantest.BuildChild(innerBlock1)
	innerVMBlks = append(innerVMBlks, innerBlock2)
	statelessBlock2, err := statelessblock.Build(
		statelessBlock1.ID(),
		statelessBlock1.Timestamp(),
		currentPChainHeight+1,
		statelessblock.Epoch{},
		pTestCert,
		innerBlock2.Bytes(),
		ctx.ChainID,
		pTestSigner,
	)
	require.NoError(err)

	block2, err := proVM.ParseBlock(t.Context(), statelessBlock2.Bytes())
	require.NoError(err)

	require.NoError(block2.Verify(t.Context()))
	require.NoError(block2.Accept(t.Context()))

	require.NoError(proVM.SetPreference(t.Context(), statelessBlock2.ID()))

	// At this point, the VM has a last accepted block with a P-chain height
	// greater than our locally accepted P-chain.
	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))

	// If the inner VM requests building a block, the proposervm passes that
	// message to the consensus engine. This is really the source of the issue,
	// as the proposervm is not currently in a state where it can correctly
	// build any blocks.
	msg, err := proVM.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)

	innerBlock3 := snowmantest.BuildChild(innerBlock2)
	innerVMBlks = append(innerVMBlks, innerBlock3)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock3, nil
	}

	// Attempting to build a block now errors with an unexpected error.
	_, err = proVM.BuildBlock(t.Context())
	require.NoError(err)
}

func TestBuildBlockFallback(t *testing.T) {
	require := require.New(t)

	coreVM, valState, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	proVM.ctx.SubnetID = ids.GenerateTestID()

	someNodeID := ids.GenerateTestNodeID()

	coreBlk0 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1 := snowmantest.BuildChild(coreBlk0)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	// First, build and accept coreBlk0 through the proposerVM so it exists in State
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk0, nil
	}

	proBlk0, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(proBlk0.Verify(t.Context()))
	require.NoError(proVM.SetPreference(t.Context(), proBlk0.ID()))
	require.NoError(proBlk0.Accept(t.Context()))

	// Now change the validator set to exclude this node
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			someNodeID: {
				NodeID: someNodeID,
				Weight: 10,
			},
		}, nil
	}

	// Set up to build coreBlk1
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	coreVM.WaitForEventF = func(context.Context) (common.Message, error) {
		return common.PendingTxs, nil
	}

	proVM.Clock.Set(coreBlk0.Timestamp().Add(time.Second))

	// ProposerVM cannot build a block because this node is not among the validator set.
	impatientContext, cancel := context.WithTimeout(t.Context(), time.Millisecond*100)
	_, err = proVM.WaitForEvent(impatientContext)
	cancel()
	require.ErrorIs(err, context.DeadlineExceeded)

	// However, it can build the block when we activate the fallback setting.
	proVM.Config.FallbackNonValidatorCanPropose = true
	proVM.Config.FallbackProposerMaxWaitTime = time.Millisecond * 100

	msg, err := proVM.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)

	statefulBlock1, err := proVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(statefulBlock1.Verify(t.Context()))
}
