// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	_ block.ChainVM              = (*fullVM)(nil)
	_ block.HeightIndexedChainVM = (*fullVM)(nil)
	_ block.StateSyncableVM      = (*fullVM)(nil)
)

type fullVM struct {
	*block.TestVM
	*block.TestHeightIndexedVM
	*block.TestStateSyncableVM
}

var (
	pTestCert *tls.Certificate

	genesisUnixTimestamp int64 = 1000
	genesisTimestamp           = time.Unix(genesisUnixTimestamp, 0)

	defaultPChainHeight uint64 = 2000

	errUnknownBlock      = errors.New("unknown block")
	errUnverifiedBlock   = errors.New("unverified block")
	errMarshallingFailed = errors.New("marshalling failed")
	errTooHigh           = errors.New("too high")
)

func init() {
	var err error
	pTestCert, err = staking.NewTLSCert()
	if err != nil {
		panic(err)
	}
}

func initTestProposerVM(
	t *testing.T,
	proBlkStartTime time.Time,
	minPChainHeight uint64,
) (
	*fullVM,
	*validators.TestState,
	*VM,
	*snowman.TestBlock,
	manager.Manager,
) {
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	initialState := []byte("genesis state")
	coreVM := &fullVM{
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

	coreVM.InitializeF = func(context.Context, *snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		proBlkStartTime,
		minPChainHeight,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return coreGenBlk.HeightV, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
			{1}: {
				NodeID: ids.NodeID{1},
				Weight: 5,
			},
			{2}: {
				NodeID: ids.NodeID{2},
				Weight: 6,
			},
			{3}: {
				NodeID: ids.NodeID{3},
				Weight: 7,
			},
		}, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.ValidatorState = valState

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})

	// signal height index is complete
	coreVM.VerifyHeightIndexF = func(context.Context) error {
		return nil
	}

	err := proVM.Initialize(
		context.Background(),
		ctx,
		dummyDBManager,
		initialState,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	if err := proVM.SetState(context.Background(), snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), coreGenBlk.IDV); err != nil {
		t.Fatal(err)
	}

	return coreVM, valState, proVM, coreGenBlk, dummyDBManager
}

// VM.BuildBlock tests section

func TestBuildBlockTimestampAreRoundedToSeconds(t *testing.T) {
	// given the same core block, BuildBlock returns the same proposer block
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	skewedTimestamp := time.Now().Truncate(time.Second).Add(time.Millisecond)
	proVM.Set(skewedTimestamp)

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	if builtBlk.Timestamp().Truncate(time.Second) != builtBlk.Timestamp() {
		t.Fatal("Timestamp should be rounded to second")
	}
}

func TestBuildBlockIsIdempotent(t *testing.T) {
	// given the same core block, BuildBlock returns the same proposer block
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk1, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	builtBlk2, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	if !bytes.Equal(builtBlk1.Bytes(), builtBlk2.Bytes()) {
		t.Fatal("proposer blocks wrapping the same core block are different")
	}
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	snowBlock, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build block")
	}

	// checks
	proBlock, ok := snowBlock.(*postForkBlock)
	if !ok {
		t.Fatal("proposerVM.BuildBlock() does not return a proposervm.Block")
	}

	if proBlock.innerBlk != coreBlk {
		t.Fatal("different block was expected to be built")
	}
}

// both core blocks and pro blocks must be built on preferred
func TestProposerBlocksAreBuiltOnPreferredProBlock(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	// add two proBlks...
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Could not build proBlk1 due to %s", err)
	}

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	if err := proBlk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	// ...and set one as preferred
	var prefcoreBlk *snowman.TestBlock
	coreVM.SetPreferenceF = func(_ context.Context, prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			prefcoreBlk = coreBlk1
			return nil
		case coreBlk2.ID():
			prefcoreBlk = coreBlk2
			return nil
		default:
			t.Fatal("Unknown core Blocks set as preferred")
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
			t.Fatalf("Wrong bytes")
			return nil, nil
		}
	}

	if err := proVM.SetPreference(context.Background(), proBlk2.ID()); err != nil {
		t.Fatal("Could not set preference")
	}

	// build block...
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    prefcoreBlk.ID(),
		HeightV:    prefcoreBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	}

	// ...show that parent is the preferred one
	if builtBlk.Parent() != proBlk2.ID() {
		t.Fatal("proposer block not built on preferred parent")
	}
}

func TestCoreBlocksMustBeBuiltOnPreferredCoreBlock(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	proBlk1, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build proBlk1")
	}

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	proBlk2, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	if err := proBlk2.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	// ...and set one as preferred
	var wronglyPreferredcoreBlk *snowman.TestBlock
	coreVM.SetPreferenceF = func(_ context.Context, prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			wronglyPreferredcoreBlk = coreBlk2
			return nil
		case coreBlk2.ID():
			wronglyPreferredcoreBlk = coreBlk1
			return nil
		default:
			t.Fatal("Unknown core Blocks set as preferred")
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
			t.Fatalf("Wrong bytes")
			return nil, nil
		}
	}

	if err := proVM.SetPreference(context.Background(), proBlk2.ID()); err != nil {
		t.Fatal("Could not set preference")
	}

	// build block...
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    wronglyPreferredcoreBlk.ID(),
		HeightV:    wronglyPreferredcoreBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	blk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err == nil {
		t.Fatal("coreVM does not build on preferred coreBlock. It should err")
	}
}

// VM.ParseBlock tests section
func TestCoreBlockFailureCauseProposerBlockParseFailure(t *testing.T) {
	coreVM, _, proVM, _, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: proVM.Time(),
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return nil, errMarshallingFailed
	}
	slb, err := statelessblock.Build(
		proVM.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		proVM.stakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.stakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk := postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
			status:   choices.Processing,
		},
	}

	// test

	if _, err := proVM.ParseBlock(context.Background(), proBlk.Bytes()); err == nil {
		t.Fatal("failed parsing proposervm.Block. Error:", err)
	}
}

func TestTwoProBlocksWrappingSameCoreBlockCanBeParsed(t *testing.T) {
	coreVM, _, proVM, gencoreBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	// create two Proposer blocks at the same height
	innerBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		ParentV:    gencoreBlk.ID(),
		HeightV:    gencoreBlk.Height() + 1,
		TimestampV: proVM.Time(),
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, innerBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return innerBlk, nil
	}

	slb1, err := statelessblock.Build(
		proVM.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		proVM.stakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.stakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk1 := postForkBlock{
		SignedBlock: slb1,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
			status:   choices.Processing,
		},
	}

	slb2, err := statelessblock.Build(
		proVM.preferred,
		innerBlk.Timestamp(),
		200, // pChainHeight,
		proVM.stakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.stakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk2 := postForkBlock{
		SignedBlock: slb2,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
			status:   choices.Processing,
		},
	}

	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("Test requires proBlk1 and proBlk2 to be different")
	}

	// Show that both can be parsed and retrieved
	parsedBlk1, err := proVM.ParseBlock(context.Background(), proBlk1.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse parsedBlk1")
	}
	parsedBlk2, err := proVM.ParseBlock(context.Background(), proBlk2.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse parsedBlk2")
	}

	if parsedBlk1.ID() != proBlk1.ID() {
		t.Fatal("error in parsing block")
	}
	if parsedBlk2.ID() != proBlk2.ID() {
		t.Fatal("error in parsing block")
	}
}

// VM.BuildBlock and VM.ParseBlock interoperability tests section
func TestTwoProBlocksWithSameParentCanBothVerify(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	// one block is built from this proVM
	localcoreBlk := &snowman.TestBlock{
		BytesV:     []byte{111},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: genesisTimestamp,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return localcoreBlk, nil
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build block")
	}
	if err := builtBlk.Verify(context.Background()); err != nil {
		t.Fatal("Built block does not verify")
	}

	// another block with same parent comes from network and is parsed
	netcoreBlk := &snowman.TestBlock{
		BytesV:     []byte{222},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: genesisTimestamp,
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, localcoreBlk.Bytes()):
			return localcoreBlk, nil
		case bytes.Equal(b, netcoreBlk.Bytes()):
			return netcoreBlk, nil
		default:
			t.Fatalf("Unknown bytes")
			return nil, nil
		}
	}

	pChainHeight, err := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	if err != nil {
		t.Fatal("could not retrieve pChain height")
	}

	netSlb, err := statelessblock.BuildUnsigned(
		proVM.preferred,
		netcoreBlk.Timestamp(),
		pChainHeight,
		netcoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	netProBlk := postForkBlock{
		SignedBlock: netSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: netcoreBlk,
			status:   choices.Processing,
		},
	}

	// prove that also block from network verifies
	if err := netProBlk.Verify(context.Background()); err != nil {
		t.Fatal("block from network does not verify")
	}
}

// Pre Fork tests section
func TestPreFork_Initialize(t *testing.T) {
	_, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks

	// checks
	blkID, err := proVM.LastAccepted(context.Background())
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	rtvdBlk, err := proVM.GetBlock(context.Background(), blkID)
	if err != nil {
		t.Fatal("Block should be returned without calling core vm")
	}

	if _, ok := rtvdBlk.(*preForkBlock); !ok {
		t.Fatal("Block retrieved from proposerVM should be proposerBlocks")
	}
	if !bytes.Equal(rtvdBlk.Bytes(), coreGenBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

func TestPreFork_BuildBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// test
	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if _, ok := builtBlk.(*preForkBlock); !ok {
		t.Fatal("Block built by proposerVM should be proposerBlocks")
	}
	if builtBlk.ID() != coreBlk.ID() {
		t.Fatal("unexpected built block")
	}
	if !bytes.Equal(builtBlk.Bytes(), coreBlk.Bytes()) {
		t.Fatal("unexpected built block")
	}

	// test
	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(context.Background(), builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk.ID() != builtBlk.ID() {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestPreFork_ParseBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, _, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		BytesV: []byte{1},
	}

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	parsedBlk, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	if err != nil {
		t.Fatal("Could not parse naked core block")
	}
	if _, ok := parsedBlk.(*preForkBlock); !ok {
		t.Fatal("Block parsed by proposerVM should be proposerBlocks")
	}
	if parsedBlk.ID() != coreBlk.ID() {
		t.Fatal("Parsed block does not match expected block")
	}
	if !bytes.Equal(parsedBlk.Bytes(), coreBlk.Bytes()) {
		t.Fatal("Parsed block does not match expected block")
	}

	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		if id != coreBlk.ID() {
			t.Fatalf("Unknown core block")
		}
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(context.Background(), parsedBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached parsed block")
	}
	if storedBlk.ID() != parsedBlk.ID() {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestPreFork_SetPreference(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks

	coreBlk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk0, nil
	}
	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	if err := proVM.SetPreference(context.Background(), builtBlk.ID()); err != nil {
		t.Fatal("Could not set preference on proposer Block")
	}

	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(444),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk0.ID(),
		HeightV:    coreBlk0.Height() + 1,
		TimestampV: coreBlk0.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	nextBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("Could not build proposer block %s", err)
	}
	if nextBlk.Parent() != builtBlk.ID() {
		t.Fatal("Preferred block should be parent of next built block")
	}
}

func TestExpiredBuildBlock(t *testing.T) {
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	coreVM := &block.TestVM{}
	coreVM.T = t

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return coreGenBlk.Height(), nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			{1}: {
				NodeID: ids.NodeID{1},
				Weight: 100,
			},
		}, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.ValidatorState = valState

	dbManager := manager.NewMemDB(version.Semantic1_0_0)
	toEngine := make(chan common.Message, 1)
	var toScheduler chan<- common.Message

	coreVM.InitializeF = func(
		_ context.Context,
		_ *snow.Context,
		_ manager.Manager,
		_ []byte,
		_ []byte,
		_ []byte,
		toEngineChan chan<- common.Message,
		_ []*common.Fx,
		_ common.AppSender,
	) error {
		toScheduler = toEngineChan
		return nil
	}

	// make sure that DBs are compressed correctly
	err := proVM.Initialize(
		context.Background(),
		ctx,
		dbManager,
		nil,
		nil,
		nil,
		toEngine,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	if err := proVM.SetState(context.Background(), snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), coreGenBlk.IDV); err != nil {
		t.Fatal(err)
	}

	// Make sure that passing a message works
	toScheduler <- common.PendingTxs
	<-toEngine

	// Notify the proposer VM of a new block on the inner block side
	toScheduler <- common.PendingTxs

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	statelessBlock, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0,
		coreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

	proVM.Clock.Set(statelessBlock.Timestamp())

	parsedBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), parsedBlock.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		t.Fatal("unexpectedly called build block")
		panic("unexpectedly called build block")
	}

	// The first notification will be read from the consensus engine
	<-toEngine

	if _, err := proVM.BuildBlock(context.Background()); err == nil {
		t.Fatal("build block when the proposer window hasn't started")
	}

	proVM.Set(statelessBlock.Timestamp().Add(proposer.MaxDelay))
	proVM.Scheduler.SetBuildBlockTime(time.Now())

	// The engine should have been notified to attempt to build a block now that
	// the window has started again
	<-toEngine
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
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreBlk0 := &wrappedBlock{
		Block: coreBlk,
	}
	coreBlk1 := &wrappedBlock{
		Block: coreBlk,
	}
	statelessBlock0, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0,
		coreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}
	statelessBlock1, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		1,
		coreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock0, err := proVM.ParseBlock(context.Background(), statelessBlock0.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock0.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), parsedBlock0.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock1, err := proVM.ParseBlock(context.Background(), statelessBlock1.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), parsedBlock1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock1.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInnerVMRollback(t *testing.T) {
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	valState := &validators.TestState{
		T: t,
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			{1}: {
				NodeID: ids.NodeID{1},
				Weight: 100,
			},
		}, nil
	}

	coreVM := &block.TestVM{}
	coreVM.T = t

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.ValidatorState = valState

	coreVM.InitializeF = func(
		context.Context,
		*snow.Context,
		manager.Manager,
		[]byte,
		[]byte,
		[]byte,
		chan<- common.Message,
		[]*common.Fx,
		common.AppSender,
	) error {
		return nil
	}

	dbManager := manager.NewMemDB(version.Semantic1_0_0)

	proVM := New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	err := proVM.Initialize(
		context.Background(),
		ctx,
		dbManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	if err := proVM.SetState(context.Background(), snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), coreGenBlk.IDV); err != nil {
		t.Fatal(err)
	}

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	statelessBlock, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0,
		coreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

	proVM.Clock.Set(statelessBlock.Timestamp())

	parsedBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if status := parsedBlock.Status(); status != choices.Processing {
		t.Fatalf("expected status to be %s but was %s", choices.Processing, status)
	}

	if err := parsedBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), parsedBlock.ID()); err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	fetchedBlock, err := proVM.GetBlock(context.Background(), parsedBlock.ID())
	if err != nil {
		t.Fatal(err)
	}

	if status := fetchedBlock.Status(); status != choices.Accepted {
		t.Fatalf("unexpected status %s. Expected %s", status, choices.Accepted)
	}

	// Restart the node and have the inner VM rollback state.

	coreBlk.StatusV = choices.Processing

	proVM = New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	err = proVM.Initialize(
		context.Background(),
		ctx,
		dbManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	lastAcceptedID, err := proVM.LastAccepted(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID != coreGenBlk.IDV {
		t.Fatalf("failed to roll back the VM to the last accepted block")
	}

	parsedBlock, err = proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if status := parsedBlock.Status(); status != choices.Processing {
		t.Fatalf("expected status to be %s but was %s", choices.Processing, status)
	}
}

func TestBuildBlockDuringWindow(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
		}, nil
	}

	coreBlk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk0.ID(),
		HeightV:    coreBlk0.Height() + 1,
		TimestampV: coreBlk0.Timestamp(),
	}
	statelessBlock0, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreBlk0.Timestamp(),
		0,
		coreBlk0.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
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
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
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
	if err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock0.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), statefulBlock0.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}

	statefulBlock1, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock1.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), statefulBlock1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock0.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock1.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	forkTime := time.Unix(0, 0)
	coreVM, _, proVM, gBlock, _ := initTestProposerVM(t, forkTime, 0)

	// create pre-fork block X and post-fork block A
	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	coreVM.BuildBlockF = nil
	if err := aBlock.Verify(context.Background()); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	// use a different way to construct pre-fork block Y and post-fork block B
	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp(),
	}

	ySlb, err := statelessblock.BuildUnsigned(
		gBlock.ID(),
		gBlock.Timestamp(),
		defaultPChainHeight,
		yBlock.Bytes(),
	)
	if err != nil {
		t.Fatalf("fail to manually build a block due to %s", err)
	}

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
			status:   choices.Processing,
		},
	}

	if err := bBlock.Verify(context.Background()); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	// append Z/C to Y/B
	zBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    yBlock.ID(),
		HeightV:    yBlock.Height() + 1,
		TimestampV: yBlock.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return zBlock, nil
	}
	if err := proVM.SetPreference(context.Background(), bBlock.ID()); err != nil {
		t.Fatal(err)
	}
	cBlock, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	coreVM.BuildBlockF = nil

	if err := cBlock.Verify(context.Background()); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	if aBlock.Parent() != bBlock.Parent() ||
		zBlock.Parent() != yBlock.ID() ||
		cBlock.Parent() != bBlock.ID() {
		t.Fatal("inconsistent parent")
	}

	if yBlock.Status() == choices.Rejected {
		t.Fatal("yBlock should not be rejected")
	}

	// accept A
	if err := aBlock.Accept(context.Background()); err != nil {
		t.Fatalf("could not accept valid block due to %s", err)
	}

	if xBlock.Status() != choices.Accepted {
		t.Fatal("xBlock should be accepted because aBlock is accepted")
	}

	if yBlock.Status() != choices.Rejected {
		t.Fatal("yBlock should be rejected")
	}
	if zBlock.Status() != choices.Rejected {
		t.Fatal("zBlock should be rejected")
	}
}

func TestTooFarAdvanced(t *testing.T) {
	forkTime := time.Unix(0, 0)
	coreVM, _, proVM, gBlock, _ := initTestProposerVM(t, forkTime, 0)

	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp(),
	}

	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    xBlock.ID(),
		HeightV:    xBlock.Height() + 1,
		TimestampV: xBlock.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	if err := aBlock.Verify(context.Background()); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	ySlb, err := statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(maxSkew),
		defaultPChainHeight,
		yBlock.Bytes(),
	)
	if err != nil {
		t.Fatalf("fail to manually build a block due to %s", err)
	}

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
			status:   choices.Processing,
		},
	}

	if err := bBlock.Verify(context.Background()); err != errProposerWindowNotStarted {
		t.Fatal("should have errored errProposerWindowNotStarted")
	}

	ySlb, err = statelessblock.BuildUnsigned(
		aBlock.ID(),
		aBlock.Timestamp().Add(proposer.MaxDelay),
		defaultPChainHeight,
		yBlock.Bytes(),
	)

	if err != nil {
		t.Fatalf("fail to manually build a block due to %s", err)
	}

	bBlock = postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
			status:   choices.Processing,
		},
	}

	if err := bBlock.Verify(context.Background()); err != errTimeTooAdvanced {
		t.Fatal("should have errored errTimeTooAdvanced")
	}
}

// Ensure that Accepting a PostForkOption (B) causes both the other option and
// the core block in the other option to be rejected.
//
//	   G
//	   |
//	  A(X)
//	 /====\
//	B(...) C(...)
//
// B(...) is B(X.opts[0])
// B(...) is C(X.opts[1])
func TestTwoOptions_OneIsAccepted(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	xBlockID := ids.GenerateTestID()
	xBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     xBlockID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlockIntf, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	aBlock, ok := aBlockIntf.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}

	opts, err := aBlock.Options(context.Background())
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := aBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	bBlock := opts[0]
	if err := bBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	cBlock := opts[1]
	if err := cBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := aBlock.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := bBlock.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	// the other pre-fork option should be rejected
	if xBlock.opts[1].Status() != choices.Rejected {
		t.Fatal("the pre-fork option block should have be rejected")
	}

	// the other post-fork option should also be rejected
	if err := cBlock.Reject(context.Background()); err != nil {
		t.Fatal("the post-fork option block should have be rejected")
	}

	if cBlock.Status() != choices.Rejected {
		t.Fatal("cBlock status should not be accepted")
	}
}

// Ensure that given the chance, built blocks will reference a lagged P-chain
// height.
func TestLaggedPChainHeight(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	innerBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock, nil
	}
	blockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	block, ok := blockIntf.(*postForkBlock)
	require.True(ok, "expected post fork block")

	pChainHeight := block.PChainHeight()
	require.Equal(pChainHeight, coreGenBlk.Height())
}

// Ensure that rejecting a block does not modify the accepted block ID for the
// rejected height.
func TestRejectedHeightNotIndexed(t *testing.T) {
	require := require.New(t)

	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	coreHeights := []ids.ID{coreGenBlk.ID()}

	initialState := []byte("genesis state")
	coreVM := &struct {
		block.TestVM
		block.TestHeightIndexedVM
	}{
		TestVM: block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
		TestHeightIndexedVM: block.TestHeightIndexedVM{
			T: t,
			VerifyHeightIndexF: func(context.Context) error {
				return nil
			},
			GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
				if height >= uint64(len(coreHeights)) {
					return ids.ID{}, errTooHigh
				}
				return coreHeights[height], nil
			},
		},
	}

	coreVM.InitializeF = func(context.Context, *snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return coreGenBlk.HeightV, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
			{1}: {
				NodeID: ids.NodeID{1},
				Weight: 5,
			},
			{2}: {
				NodeID: ids.NodeID{2},
				Weight: 6,
			},
			{3}: {
				NodeID: ids.NodeID{3},
				Weight: 7,
			},
		}, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.ValidatorState = valState

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
	err := proVM.Initialize(
		context.Background(),
		ctx,
		dummyDBManager,
		initialState,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(err)

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	err = proVM.SetState(context.Background(), snow.NormalOp)
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), coreGenBlk.IDV)
	require.NoError(err)

	ctx.Lock.Lock()
	for proVM.VerifyHeightIndex(context.Background()) != nil {
		ctx.Lock.Unlock()
		time.Sleep(time.Millisecond)
		ctx.Lock.Lock()
	}
	ctx.Lock.Unlock()

	// create inner block X and outer block A
	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.BuildBlockF = nil
	err = aBlock.Verify(context.Background())
	require.NoError(err)

	// use a different way to construct inner block Y and outer block B
	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}

	ySlb, err := statelessblock.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		defaultPChainHeight,
		yBlock.Bytes(),
	)
	require.NoError(err)

	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
			status:   choices.Processing,
		},
	}

	err = bBlock.Verify(context.Background())
	require.NoError(err)

	// accept A
	err = aBlock.Accept(context.Background())
	require.NoError(err)
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// reject B
	err = bBlock.Reject(context.Background())
	require.NoError(err)

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)
}

// Ensure that rejecting an option block does not modify the accepted block ID
// for the rejected height.
func TestRejectedOptionHeightNotIndexed(t *testing.T) {
	require := require.New(t)

	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	coreHeights := []ids.ID{coreGenBlk.ID()}

	initialState := []byte("genesis state")
	coreVM := &struct {
		block.TestVM
		block.TestHeightIndexedVM
	}{
		TestVM: block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
		TestHeightIndexedVM: block.TestHeightIndexedVM{
			T: t,
			VerifyHeightIndexF: func(context.Context) error {
				return nil
			},
			GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
				if height >= uint64(len(coreHeights)) {
					return ids.ID{}, errTooHigh
				}
				return coreHeights[height], nil
			},
		},
	}

	coreVM.InitializeF = func(context.Context, *snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return coreGenBlk.HeightV, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
			{1}: {
				NodeID: ids.NodeID{1},
				Weight: 5,
			},
			{2}: {
				NodeID: ids.NodeID{2},
				Weight: 6,
			},
			{3}: {
				NodeID: ids.NodeID{3},
				Weight: 7,
			},
		}, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.ValidatorState = valState

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
	err := proVM.Initialize(
		context.Background(),
		ctx,
		dummyDBManager,
		initialState,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(err)

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	err = proVM.SetState(context.Background(), snow.NormalOp)
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), coreGenBlk.IDV)
	require.NoError(err)

	ctx.Lock.Lock()
	for proVM.VerifyHeightIndex(context.Background()) != nil {
		ctx.Lock.Unlock()
		time.Sleep(time.Millisecond)
		ctx.Lock.Lock()
	}
	ctx.Lock.Unlock()

	xBlockID := ids.GenerateTestID()
	xBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     xBlockID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	aBlock, ok := aBlockIntf.(*postForkBlock)
	require.True(ok)

	opts, err := aBlock.Options(context.Background())
	require.NoError(err)

	err = aBlock.Verify(context.Background())
	require.NoError(err)

	bBlock := opts[0]
	err = bBlock.Verify(context.Background())
	require.NoError(err)

	cBlock := opts[1]
	err = cBlock.Verify(context.Background())
	require.NoError(err)

	// accept A
	err = aBlock.Accept(context.Background())
	require.NoError(err)
	coreHeights = append(coreHeights, xBlock.ID())

	blkID, err := proVM.GetBlockIDAtHeight(context.Background(), aBlock.Height())
	require.NoError(err)
	require.Equal(aBlock.ID(), blkID)

	// accept B
	err = bBlock.Accept(context.Background())
	require.NoError(err)
	coreHeights = append(coreHeights, xBlock.opts[0].ID())

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), bBlock.Height())
	require.NoError(err)
	require.Equal(bBlock.ID(), blkID)

	// reject C
	err = cBlock.Reject(context.Background())
	require.NoError(err)

	blkID, err = proVM.GetBlockIDAtHeight(context.Background(), cBlock.Height())
	require.NoError(err)
	require.Equal(bBlock.ID(), blkID)
}

func TestVMInnerBlkCache(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a VM
	innerVM := mocks.NewMockChainVM(ctrl)
	vm := New(
		innerVM,
		time.Time{}, // fork is active
		0,           // minimum P-Chain height
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})

	innerVM.EXPECT().Initialize(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil)

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)

	err := vm.Initialize(
		context.Background(),
		ctx,
		dummyDBManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(err)
	state := state.NewMockState(ctrl) // mock state
	vm.State = state

	// Create a block near the tip (0).
	blkNearTipInnerBytes := []byte{1}
	blkNearTip, err := statelessblock.Build(
		ids.GenerateTestID(), // parent
		time.Time{},          // timestamp
		1,                    // pChainHeight,
		vm.stakingCertLeaf,   // cert
		blkNearTipInnerBytes, // inner blk bytes
		vm.ctx.ChainID,       // chain ID
		vm.stakingLeafSigner, // key
	)
	require.NoError(err)

	// Parse a block.
	// Not in the VM's state so need to parse it.
	state.EXPECT().GetBlock(blkNearTip.ID()).Return(blkNearTip, choices.Accepted, nil).Times(2)
	// We will ask the inner VM to parse.
	mockInnerBlkNearTip := snowman.NewMockBlock(ctrl)
	mockInnerBlkNearTip.EXPECT().Height().Return(uint64(1)).Times(2)
	innerVM.EXPECT().ParseBlock(gomock.Any(), blkNearTipInnerBytes).Return(mockInnerBlkNearTip, nil).Times(2)
	_, err = vm.ParseBlock(context.Background(), blkNearTip.Bytes())
	require.NoError(err)

	// Block should now be in cache because it's a post-fork block
	// and close to the tip.
	gotBlk, ok := vm.innerBlkCache.Get(blkNearTip.ID())
	require.True(ok)
	require.Equal(mockInnerBlkNearTip, gotBlk)
	require.EqualValues(0, vm.lastAcceptedHeight)

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

func TestVMInnerBlkCacheDeduplicationRegression(t *testing.T) {
	require := require.New(t)
	forkTime := time.Unix(0, 0)
	coreVM, _, proVM, gBlock, _ := initTestProposerVM(t, forkTime, 0)

	// create pre-fork block X and post-fork block A
	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil

	bStatelessBlock, err := statelessblock.BuildUnsigned(
		gBlock.ID(),
		gBlock.Timestamp(),
		defaultPChainHeight,
		xBlock.Bytes(),
	)
	require.NoError(err)

	xBlockCopy := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     xBlock.IDV,
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp(),
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		return xBlockCopy, nil
	}

	bBlockBytes := bStatelessBlock.Bytes()
	bBlock, err := proVM.ParseBlock(context.Background(), bBlockBytes)
	require.NoError(err)

	err = aBlock.Verify(context.Background())
	require.NoError(err)

	err = bBlock.Verify(context.Background())
	require.NoError(err)

	err = aBlock.Accept(context.Background())
	require.NoError(err)

	err = bBlock.Reject(context.Background())
	require.NoError(err)

	require.Equal(
		choices.Accepted,
		aBlock.(*postForkBlock).innerBlk.Status(),
	)

	require.Equal(
		choices.Accepted,
		bBlock.(*postForkBlock).innerBlk.Status(),
	)

	cachedXBlock, ok := proVM.innerBlkCache.Get(bBlock.ID())
	require.True(ok)
	require.Equal(
		choices.Accepted,
		cachedXBlock.Status(),
	)
}

type blockWithVerifyContext struct {
	*snowman.MockBlock
	*mocks.MockWithVerifyContext
}

// Ensures that we call [VerifyWithContext] rather than [Verify] on blocks that
// implement [block.WithVerifyContext] and that returns true for
// [ShouldVerifyWithContext].
func TestVM_VerifyBlockWithContext(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create a VM
	innerVM := mocks.NewMockChainVM(ctrl)
	vm := New(
		innerVM,
		time.Time{}, // fork is active
		0,           // minimum P-Chain height
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})

	innerVM.EXPECT().Initialize(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(nil)

	snowCtx := snow.DefaultContextTest()
	snowCtx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)

	err := vm.Initialize(
		context.Background(),
		snowCtx,
		dummyDBManager,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(err)

	{
		pChainHeight := uint64(0)
		innerBlk := blockWithVerifyContext{
			MockBlock:             snowman.NewMockBlock(ctrl),
			MockWithVerifyContext: mocks.NewMockWithVerifyContext(ctrl),
		}
		innerBlk.MockWithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(2)
		innerBlk.MockWithVerifyContext.EXPECT().VerifyWithContext(context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
		).Return(nil)
		innerBlk.MockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.MockBlock.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()

		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()

		err = vm.verifyAndRecordInnerBlk(
			context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
			blk,
		)
		require.NoError(err)

		// Call VerifyWithContext again but with a different P-Chain height
		blk.EXPECT().setInnerBlk(innerBlk).AnyTimes()
		pChainHeight++
		innerBlk.MockWithVerifyContext.EXPECT().VerifyWithContext(context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
		).Return(nil)

		err = vm.verifyAndRecordInnerBlk(
			context.Background(),
			&block.Context{
				PChainHeight: pChainHeight,
			},
			blk,
		)
		require.NoError(err)
	}

	{
		// Ensure we call Verify on a block that returns
		// false for ShouldVerifyWithContext
		innerBlk := blockWithVerifyContext{
			MockBlock:             snowman.NewMockBlock(ctrl),
			MockWithVerifyContext: mocks.NewMockWithVerifyContext(ctrl),
		}
		innerBlk.MockWithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(false, nil)
		innerBlk.MockBlock.EXPECT().Verify(gomock.Any()).Return(nil)
		innerBlk.MockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.MockBlock.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()
		err = vm.verifyAndRecordInnerBlk(
			context.Background(),
			&block.Context{
				PChainHeight: 1,
			},
			blk,
		)
		require.NoError(err)
	}

	{
		// Ensure we call Verify on a block that doesn't have a valid context
		innerBlk := blockWithVerifyContext{
			MockBlock:             snowman.NewMockBlock(ctrl),
			MockWithVerifyContext: mocks.NewMockWithVerifyContext(ctrl),
		}
		innerBlk.MockBlock.EXPECT().Verify(gomock.Any()).Return(nil)
		innerBlk.MockBlock.EXPECT().Parent().Return(ids.GenerateTestID()).AnyTimes()
		innerBlk.MockBlock.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
		blk := NewMockPostForkBlock(ctrl)
		blk.EXPECT().getInnerBlk().Return(innerBlk).AnyTimes()
		blkID := ids.GenerateTestID()
		blk.EXPECT().ID().Return(blkID).AnyTimes()
		err = vm.verifyAndRecordInnerBlk(context.Background(), nil, blk)
		require.NoError(err)
	}
}
