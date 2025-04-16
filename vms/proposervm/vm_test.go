// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler/schedulermock"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	_ block.ChainVM         = (*fullVM)(nil)
	_ block.StateSyncableVM = (*fullVM)(nil)
)

type fullVM struct {
	*blocktest.VM
	*blocktest.StateSyncableVM
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

func initTestProposerVM(
	t *testing.T,
	proBlkStartTime time.Time,
	durangoTime time.Time,
	minPChainHeight uint64,
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
			},
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
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
			Upgrades: upgrade.Config{
				ApricotPhase4Time:            proBlkStartTime,
				ApricotPhase4MinPChainHeight: minPChainHeight,
				DurangoTime:                  durangoTime,
			},
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

	ctx := snowtest.Context(t, ids.ID{1})
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	db := prefixdb.New([]byte{0}, memdb.New())

	require.NoError(proVM.Initialize(
		context.Background(),
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

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))

	proVM.Set(snowmantest.GenesisTimestamp)

	return coreVM, valState, proVM, db
}

func waitForProposerWindow(vm *VM, chainTip snowman.Block, pchainHeight uint64) error {
	var (
		ctx              = context.Background()
		childBlockHeight = chainTip.Height() + 1
		parentTimestamp  = chainTip.Timestamp()
	)

	for {
		slot := proposer.TimeToSlot(parentTimestamp, vm.Clock.Time().Truncate(time.Second))
		delay, err := vm.MinDelayForProposer(
			ctx,
			childBlockHeight,
			pchainHeight,
			vm.ctx.NodeID,
			slot,
		)
		if err != nil {
			return err
		}

		vm.Clock.Set(parentTimestamp.Add(delay))
		if delay < proposer.MaxLookAheadWindow {
			return nil
		}
	}
}

// VM.BuildBlock tests section

func TestBuildBlockTimestampAreRoundedToSeconds(t *testing.T) {
	require := require.New(t)

	// given the same core block, BuildBlock returns the same proposer block
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	skewedTimestamp := time.Now().Truncate(time.Second).Add(time.Millisecond)
	proVM.Set(skewedTimestamp)

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.Equal(builtBlk.Timestamp().Truncate(time.Second), builtBlk.Timestamp())
}

func TestBuildBlockIsIdempotent(t *testing.T) {
	require := require.New(t)

	// given the same core block, BuildBlock returns the same proposer block
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// Mock the clock time to make sure that block timestamps will be equal
	proVM.Clock.Set(time.Now())

	builtBlk1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	builtBlk2, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.Equal(builtBlk1.Bytes(), builtBlk2.Bytes())
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	require := require.New(t)

	// setup
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	snowBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// checks
	require.IsType(&postForkBlock{}, snowBlock)
	proBlock := snowBlock.(*postForkBlock)

	require.Equal(coreBlk, proBlock.innerBlk)
}

// both core blocks and pro blocks must be built on preferred
func TestProposerBlocksAreBuiltOnPreferredProBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// add two proBlks...
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	coreBlk2 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NotEqual(proBlk2.ID(), proBlk1.ID())
	require.NoError(proBlk2.Verify(context.Background()))

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

	require.NoError(proVM.SetPreference(context.Background(), proBlk2.ID()))

	// build block...
	coreBlk3 := snowmantest.BuildChild(prefcoreBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	require.NoError(waitForProposerWindow(proVM, proBlk2, proBlk2.(*postForkBlock).PChainHeight()))
	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// ...show that parent is the preferred one
	require.Equal(proBlk2.ID(), builtBlk.Parent())
}

func TestCoreBlocksMustBeBuiltOnPreferredCoreBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	coreBlk2 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NotEqual(proBlk1.ID(), proBlk2.ID())

	require.NoError(proBlk2.Verify(context.Background()))

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

	require.NoError(proVM.SetPreference(context.Background(), proBlk2.ID()))

	// build block...
	coreBlk3 := snowmantest.BuildChild(wronglyPreferredcoreBlk)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	require.NoError(waitForProposerWindow(proVM, proBlk2, proBlk2.(*postForkBlock).PChainHeight()))
	blk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = blk.Verify(context.Background())
	require.ErrorIs(err, errInnerParentMismatch)
}

// VM.ParseBlock tests section
func TestCoreBlockFailureCauseProposerBlockParseFailure(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return nil, errMarshallingFailed
	}

	innerBlk := snowmantest.BuildChild(snowmantest.Genesis)
	slb, err := statelessblock.Build(
		proVM.preferred,
		proVM.Time(),
		100, // pChainHeight,
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
	_, err = proVM.ParseBlock(context.Background(), proBlk.Bytes())
	require.ErrorIs(err, errMarshallingFailed)
}

func TestTwoProBlocksWrappingSameCoreBlockCanBeParsed(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
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
	parsedBlk1, err := proVM.ParseBlock(context.Background(), proBlk1.Bytes())
	require.NoError(err)
	parsedBlk2, err := proVM.ParseBlock(context.Background(), proBlk2.Bytes())
	require.NoError(err)

	require.Equal(proBlk1.ID(), parsedBlk1.ID())
	require.Equal(proBlk2.ID(), parsedBlk2.ID())
}

// VM.BuildBlock and VM.ParseBlock interoperability tests section
func TestTwoProBlocksWithSameParentCanBothVerify(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// one block is built from this proVM
	localcoreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return localcoreBlk, nil
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(builtBlk.Verify(context.Background()))

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

	pChainHeight, err := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	require.NoError(err)

	netSlb, err := statelessblock.BuildUnsigned(
		proVM.preferred,
		proVM.Time(),
		pChainHeight,
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
	require.NoError(netProBlk.Verify(context.Background()))
}

// Pre Fork tests section
func TestPreFork_Initialize(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// checks
	blkID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)

	rtvdBlk, err := proVM.GetBlock(context.Background(), blkID)
	require.NoError(err)

	require.IsType(&preForkBlock{}, rtvdBlk)
	require.Equal(snowmantest.GenesisBytes, rtvdBlk.Bytes())
}

func TestPreFork_BuildBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk)
	require.Equal(coreBlk.ID(), builtBlk.ID())
	require.Equal(coreBlk.Bytes(), builtBlk.Bytes())

	// test
	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(context.Background(), builtBlk.ID())
	require.NoError(err)
	require.Equal(builtBlk.ID(), storedBlk.ID())
}

func TestPreFork_ParseBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		require.Equal(coreBlk.Bytes(), b)
		return coreBlk, nil
	}

	parsedBlk, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	require.NoError(err)
	require.IsType(&preForkBlock{}, parsedBlk)
	require.Equal(coreBlk.ID(), parsedBlk.ID())
	require.Equal(coreBlk.Bytes(), parsedBlk.Bytes())

	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		require.Equal(coreBlk.ID(), id)
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(context.Background(), parsedBlk.ID())
	require.NoError(err)
	require.Equal(parsedBlk.ID(), storedBlk.ID())
}

func TestPreFork_SetPreference(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk0 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk0, nil
	}
	builtBlk, err := proVM.BuildBlock(context.Background())
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
	require.NoError(proVM.SetPreference(context.Background(), builtBlk.ID()))

	coreBlk1 := snowmantest.BuildChild(coreBlk0)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	nextBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.Equal(builtBlk.ID(), nextBlk.Parent())
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

	testEnd := make(chan struct{})

	events := make(chan common.Message, 1)
	coreVM.SubscribeToEventsF = func(context.Context) common.Message {
		select {
		case <-testEnd:
			return 0
		case event := <-events:
			fmt.Println(">>>>>", event)
			return event
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
		context.Background(),
		ctx,
		memdb.New(),
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	defer close(testEnd)

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))

	// Notify the proposer VM of a new block on the inner block side
	events <- common.PendingTxs
	// The first notification will be read from the consensus engine
	msg := proVM.SubscribeToEvents(context.Background())
	require.Equal(common.PendingTxs, msg)

	// Before calling BuildBlock, verify a remote block and set it as the
	// preferred block.
	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	statelessBlock, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		0,
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

	parsedBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parsedBlock.ID()))

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		require.FailNow(fmt.Errorf("%w: BuildBlock", errUnexpectedCall).Error())
		return nil, errUnexpectedCall
	}

	// Because we are now building on a different block, the proposer window
	// shouldn't have started.
	_, err = proVM.BuildBlock(context.Background())
	require.ErrorIs(err, errProposerWindowNotStarted)

	proVM.Set(statelessBlock.Timestamp().Add(proposer.MaxBuildDelay))
	proVM.Scheduler.SetBuildBlockTime(time.Now())

	// The engine should have been notified to attempt to build a block now that
	// the window has started again. This is to guarantee that the inner VM has
	// build block called after it sent a pendingTxs message on its internal
	// engine channel.
	msg = proVM.SubscribeToEvents(context.Background())
	require.Equal(common.PendingTxs, msg)
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

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
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
		coreBlk.Bytes(),
	)
	require.NoError(err)
	statelessBlock1, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		1,
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

	parsedBlock0, err := proVM.ParseBlock(context.Background(), statelessBlock0.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock0.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), parsedBlock0.ID()))

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

	parsedBlock1, err := proVM.ParseBlock(context.Background(), statelessBlock1.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock1.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), parsedBlock1.ID()))

	require.NoError(parsedBlock1.Accept(context.Background()))
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
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	statelessBlock, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		coreBlk.Timestamp(),
		0,
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

	lastAcceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(snowmantest.GenesisID, lastAcceptedID)

	parsedBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(parsedBlock.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parsedBlock.ID()))
	require.NoError(parsedBlock.Accept(context.Background()))

	lastAcceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(parsedBlock.ID(), lastAcceptedID)

	// Restart the node and have the inner VM rollback state.
	require.NoError(proVM.Shutdown(context.Background()))
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
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	lastAcceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(snowmantest.GenesisID, lastAcceptedID)
}

func TestBuildBlockDuringWindow(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
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

	statefulBlock0, err := proVM.ParseBlock(context.Background(), statelessBlock0.Bytes())
	require.NoError(err)

	require.NoError(statefulBlock0.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), statefulBlock0.ID()))

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}

	statefulBlock1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(statefulBlock1.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), statefulBlock1.ID()))

	require.NoError(statefulBlock0.Accept(context.Background()))

	require.NoError(statefulBlock1.Accept(context.Background()))
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

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre-fork block X and post-fork block A
	xBlock := snowmantest.BuildChild(snowmantest.Genesis)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil
	require.NoError(aBlock.Verify(context.Background()))

	// use a different way to construct pre-fork block Y and post-fork block B
	yBlock := snowmantest.BuildChild(snowmantest.Genesis)

	ySlb, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		proVM.Time(),
		defaultPChainHeight,
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

	require.NoError(bBlock.Verify(context.Background()))

	// append Z/C to Y/B
	zBlock := snowmantest.BuildChild(yBlock)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return zBlock, nil
	}
	require.NoError(proVM.SetPreference(context.Background(), bBlock.ID()))
	proVM.Set(proVM.Time().Add(proposer.MaxBuildDelay))
	cBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil

	require.NoError(cBlock.Verify(context.Background()))

	require.Equal(bBlock.Parent(), aBlock.Parent())
	require.Equal(yBlock.ID(), zBlock.Parent())
	require.Equal(bBlock.ID(), cBlock.Parent())

	require.NotEqual(snowtest.Rejected, yBlock.Status)

	// accept A
	require.NoError(aBlock.Accept(context.Background()))

	require.Equal(snowtest.Accepted, xBlock.Status)
	require.Equal(snowtest.Rejected, yBlock.Status)
	require.Equal(snowtest.Rejected, zBlock.Status)
}

func TestTooFarAdvanced(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	xBlock := snowmantest.BuildChild(snowmantest.Genesis)
	yBlock := snowmantest.BuildChild(xBlock)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(aBlock.Verify(context.Background()))

	ySlb, err := statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(maxSkew),
		defaultPChainHeight,
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

	err = bBlock.Verify(context.Background())
	require.ErrorIs(err, errProposerWindowNotStarted)

	ySlb, err = statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(proposer.MaxVerifyDelay),
		defaultPChainHeight,
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

	err = bBlock.Verify(context.Background())
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

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
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
	aBlockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)

	opts, err := aBlock.Options(context.Background())
	require.NoError(err)

	bBlock := opts[0]
	cBlock := opts[1]

	require.NoError(aBlock.Verify(context.Background()))
	require.NoError(bBlock.Verify(context.Background()))
	require.NoError(cBlock.Verify(context.Background()))

	require.NoError(aBlock.Accept(context.Background()))
	require.NoError(bBlock.Accept(context.Background()))

	// the other pre-fork option should be rejected
	require.Equal(snowtest.Rejected, xBlock.opts[1].Status)
}

// Ensure that given the chance, built blocks will reference a lagged P-chain
// height.
func TestLaggedPChainHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	innerBlock := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock, nil
	}
	blockIntf, err := proVM.BuildBlock(context.Background())
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
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))

	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))

	// create inner block X and outer block A
	xBlock := snowmantest.BuildChild(snowmantest.Genesis)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.BuildBlockF = nil
	require.NoError(aBlock.Verify(context.Background()))

	// use a different way to construct inner block Y and outer block B
	yBlock := snowmantest.BuildChild(snowmantest.Genesis)

	ySlb, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		defaultPChainHeight,
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

	require.NoError(bBlock.Verify(context.Background()))

	// accept A
	require.NoError(aBlock.Accept(context.Background()))
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// reject B
	require.NoError(bBlock.Reject(context.Background()))

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
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
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))

	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))

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
	aBlockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)

	opts, err := aBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(aBlock.Verify(context.Background()))

	bBlock := opts[0]
	require.NoError(bBlock.Verify(context.Background()))

	cBlock := opts[1]
	require.NoError(cBlock.Verify(context.Background()))

	// accept A
	require.NoError(aBlock.Accept(context.Background()))
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// accept B
	require.NoError(bBlock.Accept(context.Background()))
	coreHeights = append(coreHeights, xBlock.opts[0].ID())

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), bBlock.Height())
	require.NoError(err)
	require.Equal(bBlock.ID(), blkID)

	// reject C
	require.NoError(cBlock.Reject(context.Background()))

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), cBlock.Height())
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

	innerVM.EXPECT().SubscribeToEvents(gomock.Any()).Return(common.PendingTxs).AnyTimes()

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
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	// Create a block near the tip (0).
	blkNearTipInnerBytes := []byte{1}
	blkNearTip, err := statelessblock.Build(
		ids.GenerateTestID(), // parent
		time.Time{},          // timestamp
		1,                    // pChainHeight,
		vm.StakingCertLeaf,   // cert
		blkNearTipInnerBytes, // inner blk bytes
		vm.ctx.ChainID,       // chain ID
		vm.StakingLeafSigner, // key
	)
	require.NoError(err)

	// We will ask the inner VM to parse.
	mockInnerBlkNearTip := snowmanmock.NewBlock(ctrl)
	mockInnerBlkNearTip.EXPECT().Height().Return(uint64(1)).Times(2)
	mockInnerBlkNearTip.EXPECT().Bytes().Return(blkNearTipInnerBytes).Times(1)

	innerVM.EXPECT().ParseBlock(gomock.Any(), blkNearTipInnerBytes).Return(mockInnerBlkNearTip, nil).Times(2)
	_, err = vm.ParseBlock(context.Background(), blkNearTip.Bytes())
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
	_, err = vm.ParseBlock(context.Background(), blkNearTip.Bytes())
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
	innerVM.EXPECT().SubscribeToEvents(gomock.Any()).Return(common.Message(0)).AnyTimes()

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
		context.Background(),
		snowCtx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	{
		pChainHeight := uint64(0)
		innerBlk := blockWithVerifyContext{
			Block:             snowmanmock.NewBlock(ctrl),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		innerBlk.WithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(2)
		innerBlk.WithVerifyContext.EXPECT().VerifyWithContext(context.Background(),
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
			context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
			blk,
		))

		// Call VerifyWithContext again but with a different P-Chain height
		blk.EXPECT().setInnerBlk(innerBlk).AnyTimes()
		pChainHeight++
		innerBlk.WithVerifyContext.EXPECT().VerifyWithContext(context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
		).Return(nil)

		require.NoError(vm.verifyAndRecordInnerBlk(
			context.Background(),
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
			context.Background(),
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
		require.NoError(vm.verifyAndRecordInnerBlk(context.Background(), nil, blk))
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
		context.Background(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	lastAcceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), lastAcceptedID))

	issueBlock := func() {
		lastAcceptedBlock := acceptedBlocks[currentHeight]
		innerBlock := snowmantest.BuildChild(lastAcceptedBlock)

		coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
			return innerBlock, nil
		}
		proBlock, err := proVM.BuildBlock(context.Background())
		require.NoError(err)

		require.NoError(proBlock.Verify(context.Background()))
		require.NoError(proVM.SetPreference(context.Background(), proBlock.ID()))
		require.NoError(proBlock.Accept(context.Background()))

		acceptedBlocks = append(acceptedBlocks, innerBlock)
		currentHeight++
	}

	requireHeights := func(start, end uint64) {
		for i := start; i <= end; i++ {
			_, err := proVM.GetBlockIDAtHeight(context.Background(), i)
			require.NoError(err)
		}
	}

	requireMissingHeights := func(start, end uint64) {
		for i := start; i <= end; i++ {
			_, err := proVM.GetBlockIDAtHeight(context.Background(), i)
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

	require.NoError(proVM.Shutdown(context.Background()))

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
		context.Background(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	lastAcceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), lastAcceptedID))

	// Verify that old blocks were pruned during startup
	requireNumHeights(numHistoricalBlocks)

	// As we issue new blocks, the oldest indexed height should be pruned.
	issueBlock()
	requireNumHeights(numHistoricalBlocks)

	issueBlock()
	requireNumHeights(numHistoricalBlocks)

	require.NoError(proVM.Shutdown(context.Background()))

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
		context.Background(),
		ctx,
		db,
		initialState,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	lastAcceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), lastAcceptedID))

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

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
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

	statefulBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(statefulBlock.Verify(context.Background()))

	currentTime := proVM.Clock.Time().Truncate(time.Second)
	parentTimestamp := statefulBlock.Timestamp()
	slotTime, err := proVM.getPostDurangoSlotTime(
		context.Background(),
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

	innerVM.VM.SubscribeToEventsF = func(_ context.Context) common.Message {
		return common.PendingTxs
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
	ctrl := gomock.NewController(t)
	scheduler := schedulermock.NewScheduler(ctrl)
	scheduler.EXPECT().Close().AnyTimes()

	subscriber := NewMockSelfSubscriber(ctrl)
	subscriber.EXPECT().Close().AnyTimes()

	vm.Scheduler = scheduler
	vm.subscriber = subscriber

	defer func() {
		require.NoError(t, vm.Shutdown(context.Background()))
	}()

	db := prefixdb.New([]byte{}, memdb.New())

	_ = vm.Initialize(context.Background(), &snow.Context{
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
			block, err := test.f(context.Background(), test.block)
			require.NoError(t, err)
			require.IsType(t, test.resultingBlock, block)
		})
	}
}

func TestTimestampMetrics(t *testing.T) {
	ctx := context.Background()

	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0), mockable.MaxTime, 0)

	defer func() {
		require.NoError(t, proVM.Shutdown(ctx))
	}()

	innerBlock := snowmantest.BuildChild(snowmantest.Genesis)

	outerTime := time.Unix(314159, 0)
	innerTime := time.Unix(142857, 0)
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
			require.InDelta(t, float64(tt.want.Unix()), testutil.ToFloat64(gauge), 0)
		})
	}
}

func TestSelectChildPChainHeight(t *testing.T) {
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime

		beforeOverrideEnds = fujiOverridePChainHeightUntilTimestamp.Add(-time.Minute)
	)
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

			_, vdrState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
			defer func() {
				require.NoError(proVM.Shutdown(context.Background()))
			}()

			proVM.Clock.Set(test.time)
			proVM.ctx.NetworkID = test.networkID
			proVM.ctx.SubnetID = test.subnetID

			vdrState.GetMinimumHeightF = func(context.Context) (uint64, error) {
				return test.currentPChainHeight, nil
			}

			actualPChainHeight, err := proVM.selectChildPChainHeight(
				context.Background(),
				test.minPChainHeight,
			)
			require.NoError(err)
			require.Equal(test.expectedPChainHeight, actualPChainHeight)
		})
	}
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
			Upgrades: upgrade.Config{
				ApricotPhase4Time:            snowmantest.GenesisTimestamp,
				ApricotPhase4MinPChainHeight: 0,
				DurangoTime:                  snowmantest.GenesisTimestamp,
			},
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
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	require.NoError(proVM.SetState(context.Background(), snow.Bootstrapping))

	// During bootstrapping, the first post-fork block is verified against the
	// P-chain height, so we provide a valid height.
	innerBlock1 := snowmantest.BuildChild(snowmantest.Genesis)
	innerVMBlks = append(innerVMBlks, innerBlock1)
	statelessBlock1, err := statelessblock.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		currentPChainHeight,
		innerBlock1.Bytes(),
	)
	require.NoError(err)

	block1, err := proVM.ParseBlock(context.Background(), statelessBlock1.Bytes())
	require.NoError(err)

	require.NoError(block1.Verify(context.Background()))
	require.NoError(block1.Accept(context.Background()))

	// During bootstrapping, the additional post-fork blocks are not verified
	// against the local P-chain height, so even if we provide a height higher
	// than our P-chain height, verification will succeed.
	innerBlock2 := snowmantest.BuildChild(innerBlock1)
	innerVMBlks = append(innerVMBlks, innerBlock2)
	statelessBlock2, err := statelessblock.Build(
		statelessBlock1.ID(),
		statelessBlock1.Timestamp(),
		currentPChainHeight+1,
		pTestCert,
		innerBlock2.Bytes(),
		ctx.ChainID,
		pTestSigner,
	)
	require.NoError(err)

	block2, err := proVM.ParseBlock(context.Background(), statelessBlock2.Bytes())
	require.NoError(err)

	require.NoError(block2.Verify(context.Background()))
	require.NoError(block2.Accept(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), statelessBlock2.ID()))

	// At this point, the VM has a last accepted block with a P-chain height
	// greater than our locally accepted P-chain.
	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))

	// If the inner VM requests building a block, the proposervm passes that
	// message to the consensus engine. This is really the source of the issue,
	// as the proposervm is not currently in a state where it can correctly
	// build any blocks.
	msg := proVM.SubscribeToEvents(context.Background())
	require.Equal(common.PendingTxs, msg)

	innerBlock3 := snowmantest.BuildChild(innerBlock2)
	innerVMBlks = append(innerVMBlks, innerBlock3)

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock3, nil
	}

	// Attempting to build a block now errors with an unexpected error. This
	// results in dropping the build block request, which breaks the invariant
	// that BuildBlock will be called at least once after sending a PendingTxs
	// message on the ToEngine channel.
	_, err = proVM.BuildBlock(context.Background())
	require.NoError(err)
}
