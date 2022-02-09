// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	pTestCert *tls.Certificate

	genesisUnixTimestamp int64 = 1000
	genesisTimestamp           = time.Unix(genesisUnixTimestamp, 0)

	defaultPChainHeight uint64 = 2000

	errUnknownBlock      = errors.New("unknown block")
	errUnverifiedBlock   = errors.New("unverified block")
	errMarshallingFailed = errors.New("marshalling failed")
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
	*block.TestVM,
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
	coreVM := &block.TestVM{
		TestVM: common.TestVM{
			T: t,
		},
	}

	coreVM.InitializeF = func(*snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(coreVM, proBlkStartTime, minPChainHeight, false)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetCurrentHeightF = func() (uint64, error) { return defaultPChainHeight, nil }
	valState.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[proVM.ctx.NodeID] = uint64(10)
		res[ids.ShortID{1}] = uint64(5)
		res[ids.ShortID{2}] = uint64(6)
		res[ids.ShortID{3}] = uint64(7)
		return res, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = hashing.ComputeHash160Array(hashing.ComputeHash256(pTestCert.Leaf.Raw))
	ctx.StakingCertLeaf = pTestCert.Leaf
	ctx.StakingLeafSigner = pTestCert.PrivateKey.(crypto.Signer)
	ctx.ValidatorState = valState

	dummyDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
	if err := proVM.Initialize(ctx, dummyDBManager, initialState, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	if err := proVM.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(coreGenBlk.IDV); err != nil {
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	builtBlk2, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	snowBlock, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	if err := proBlk2.Verify(); err != nil {
		t.Fatal(err)
	}

	// ...and set one as preferred
	var prefcoreBlk *snowman.TestBlock
	coreVM.SetPreferenceF = func(prefID ids.ID) error {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	builtBlk, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	if err := proBlk2.Verify(); err != nil {
		t.Fatal(err)
	}

	// ...and set one as preferred
	var wronglyPreferredcoreBlk *snowman.TestBlock
	coreVM.SetPreferenceF = func(prefID ids.ID) error {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	blk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err == nil {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		return nil, errMarshallingFailed
	}
	slb, err := statelessblock.Build(
		proVM.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		proVM.ctx.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
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

	if _, err := proVM.ParseBlock(proBlk.Bytes()); err == nil {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, innerBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return innerBlk, nil
	}

	slb1, err := statelessblock.Build(
		proVM.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		proVM.ctx.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
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
		proVM.ctx.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
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
	parsedBlk1, err := proVM.ParseBlock(proBlk1.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse parsedBlk1")
	}
	parsedBlk2, err := proVM.ParseBlock(proBlk2.Bytes())
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
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		return localcoreBlk, nil
	}

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}
	if err = builtBlk.Verify(); err != nil {
		t.Fatal("Built block does not verify")
	}

	// another block with same parent comes from network and is parsed
	netcoreBlk := &snowman.TestBlock{
		BytesV:     []byte{222},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: genesisTimestamp,
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	pChainHeight, err := proVM.ctx.ValidatorState.GetCurrentHeight()
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
	if err = netProBlk.Verify(); err != nil {
		t.Fatal("block from network does not verify")
	}
}

// Pre Fork tests section
func TestPreFork_Initialize(t *testing.T) {
	_, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	rtvdBlk, err := proVM.GetBlock(blkID)
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
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
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) { return coreBlk, nil }
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
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

	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	parsedBlk, err := proVM.ParseBlock(coreBlk.Bytes())
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

	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id != coreBlk.ID() {
			t.Fatalf("Unknown core block")
		}
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk0, nil }
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	if err = proVM.SetPreference(builtBlk.ID()); err != nil {
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
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	nextBlk, err := proVM.BuildBlock()
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

	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(coreVM, time.Time{}, 0, false)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetCurrentHeightF = func() (uint64, error) { return defaultPChainHeight, nil }
	valState.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		return map[ids.ShortID]uint64{
			{1}: 100,
		}, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = hashing.ComputeHash160Array(hashing.ComputeHash256(pTestCert.Leaf.Raw))
	ctx.StakingCertLeaf = pTestCert.Leaf
	ctx.StakingLeafSigner = pTestCert.PrivateKey.(crypto.Signer)
	ctx.ValidatorState = valState

	dbManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	toEngine := make(chan common.Message, 1)
	var toScheduler chan<- common.Message

	coreVM.InitializeF = func(
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
	if err := proVM.Initialize(ctx, dbManager, nil, nil, nil, toEngine, nil, nil); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	if err := proVM.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(coreGenBlk.IDV); err != nil {
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

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	parsedBlock, err := proVM.ParseBlock(statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(parsedBlock.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) {
		t.Fatal("unexpectedly called build block")
		panic("unexpectedly called build block")
	}

	// The first notification will be read from the consensus engine
	<-toEngine

	if _, err := proVM.BuildBlock(); err == nil {
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

func (b *wrappedBlock) Accept() error {
	if !b.verified {
		return errUnverifiedBlock
	}
	return b.Block.Accept()
}

func (b *wrappedBlock) Verify() error {
	if err := b.Block.Verify(); err != nil {
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

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock0, err := proVM.ParseBlock(statelessBlock0.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock0.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(parsedBlock0.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parsedBlock1, err := proVM.ParseBlock(statelessBlock1.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock1.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(parsedBlock1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock1.Accept(); err != nil {
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
	valState.GetCurrentHeightF = func() (uint64, error) { return defaultPChainHeight, nil }
	valState.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		return map[ids.ShortID]uint64{
			{1}: 100,
		}, nil
	}

	coreVM := &block.TestVM{}
	coreVM.T = t

	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = hashing.ComputeHash160Array(hashing.ComputeHash256(pTestCert.Leaf.Raw))
	ctx.StakingCertLeaf = pTestCert.Leaf
	ctx.StakingLeafSigner = pTestCert.PrivateKey.(crypto.Signer)
	ctx.ValidatorState = valState

	coreVM.InitializeF = func(
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

	dbManager := manager.NewMemDB(version.DefaultVersion1_0_0)

	proVM := New(coreVM, time.Time{}, 0, false)

	if err := proVM.Initialize(ctx, dbManager, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	if err := proVM.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(coreGenBlk.IDV); err != nil {
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

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	parsedBlock, err := proVM.ParseBlock(statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if status := parsedBlock.Status(); status != choices.Processing {
		t.Fatalf("expected status to be %s but was %s", choices.Processing, status)
	}

	if err := parsedBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(parsedBlock.ID()); err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Accept(); err != nil {
		t.Fatal(err)
	}

	// Restart the node and have the inner VM rollback state.

	coreBlk.StatusV = choices.Processing

	proVM = New(coreVM, time.Time{}, 0, false)

	if err := proVM.Initialize(ctx, dbManager, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	lastAcceptedID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}

	if lastAcceptedID != coreGenBlk.IDV {
		t.Fatalf("failed to roll back the VM to the last accepted block")
	}

	parsedBlock, err = proVM.ParseBlock(statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if status := parsedBlock.Status(); status != choices.Processing {
		t.Fatalf("expected status to be %s but was %s", choices.Processing, status)
	}
}

func TestBuildBlockDuringWindow(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	valState.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		return map[ids.ShortID]uint64{
			proVM.ctx.NodeID: 10,
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

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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

	statefulBlock0, err := proVM.ParseBlock(statelessBlock0.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock0.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(statefulBlock0.ID()); err != nil {
		t.Fatal(err)
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) {
		return coreBlk1, nil
	}

	statefulBlock1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock1.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(statefulBlock1.ID()); err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock0.Accept(); err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock1.Accept(); err != nil {
		t.Fatal(err)
	}
}

// Ensure that Accepting a PostForkBlock (A) containing core block (X) causes
// core block (Y) and (Z) to also be rejected.
//      G
//    /   \
// A(X)   B(Y)
//         |
//        C(Z)
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

	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	aBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	coreVM.BuildBlockF = nil
	if err := aBlock.Verify(); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	// use a different way to constrcut pre-fork block Y and post-fork block B
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

	if err := bBlock.Verify(); err != nil {
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

	coreVM.BuildBlockF = func() (snowman.Block, error) { return zBlock, nil }
	if err := proVM.SetPreference(bBlock.ID()); err != nil {
		t.Fatal(err)
	}
	cBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	coreVM.BuildBlockF = nil

	if err := cBlock.Verify(); err != nil {
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
	if err := aBlock.Accept(); err != nil {
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

	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	aBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}
	if err := aBlock.Verify(); err != nil {
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

	if err = bBlock.Verify(); err != errProposerWindowNotStarted {
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

	if err = bBlock.Verify(); err != errTimeTooAdvanced {
		t.Fatal("should have errored errTimeTooAdvanced")
	}
}

// Ensure that Accepting a PostForkOption (B) causes both the other option and
// the core block in the other option to be rejected.
//     G
//     |
//    A(X)
//   /====\
//  B(...) C(...)
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

	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	aBlockIntf, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	aBlock, ok := aBlockIntf.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}

	opts, err := aBlock.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := aBlock.Verify(); err != nil {
		t.Fatal(err)
	}
	bBlock := opts[0]
	if err := bBlock.Verify(); err != nil {
		t.Fatal(err)
	}
	cBlock := opts[1]
	if err := cBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := aBlock.Accept(); err != nil {
		t.Fatal(err)
	}

	if err := bBlock.Accept(); err != nil {
		t.Fatal(err)
	}

	// the other pre-fork option should be rejected
	if xBlock.opts[1].Status() != choices.Rejected {
		t.Fatal("the pre-fork option block should have be rejected")
	}

	// the other post-fork option should also be rejected
	if err := cBlock.Reject(); err != nil {
		t.Fatal("the post-fork option block should have be rejected")
	}

	if cBlock.Status() != choices.Rejected {
		t.Fatal("cBlock status should not be accepted")
	}
}

// Ensure that given the chance, built blocks will reference a lagged P-chain
// height.
func TestLaggedPChainHeight(t *testing.T) {
	assert := assert.New(t)

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

	coreVM.BuildBlockF = func() (snowman.Block, error) { return innerBlock, nil }
	blockIntf, err := proVM.BuildBlock()
	assert.NoError(err)

	block, ok := blockIntf.(*postForkBlock)
	assert.True(ok, "expected post fork block")

	pChainHeight := block.PChainHeight()
	assert.Equal(pChainHeight, defaultPChainHeight-optimalHeightDelay)
}
