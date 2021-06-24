// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"testing"
	"time"

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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var (
	pTestCert *tls.Certificate

	genesisUnixTimestamp int64 = 1000
	genesisTimestamp           = time.Unix(genesisUnixTimestamp, 0)
)

func init() {
	var err error
	pTestCert, err = staking.NewTLSCert()
	if err != nil {
		panic(err)
	}
}

func initTestProposerVM(t *testing.T, proBlkStartTime time.Time) (*block.TestVM, *validators.TestVM, *VM, *snowman.TestBlock) {
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
	coreVM := &block.TestVM{}
	coreVM.T = t

	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return coreGenBlk, nil }

	proVM := New(coreVM, proBlkStartTime)

	valVM := &validators.TestVM{
		T: t,
	}
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }
	valVM.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[proVM.ctx.NodeID] = uint64(10)
		res[ids.ShortID{1}] = uint64(5)
		res[ids.ShortID{2}] = uint64(6)
		res[ids.ShortID{3}] = uint64(7)
		return res, nil
	}

	ctx := &snow.Context{
		NodeID:      hashing.ComputeHash160Array(hashing.ComputeHash256(pTestCert.Leaf.Raw)),
		Log:         logging.NoLog{},
		StakingCert: *pTestCert,
		ValidatorVM: valVM,
	}
	dummyDBManager := manager.NewDefaultMemDBManager()
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
	if err := proVM.Initialize(ctx, dummyDBManager, initialState, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.CantInitialize = true
	coreVM.InitializeF = nil

	return coreVM, valVM, proVM, coreGenBlk
}

// VM.BuildBlock tests section
func TestBuildBlockIsIdempotent(t *testing.T) {
	// given the same core block, BuildBlock returns the same proposer block
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
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
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
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
	proBlock, ok := snowBlock.(*ProposerBlock)
	if !ok {
		t.Fatal("proposerVM.BuildBlock() does not return a proposervm.Block")
	}

	if proBlock.coreBlk != coreBlk {
		t.Fatal("different block was expected to be built")
	}
}

// both core blocks and pro blocks must be built on preferred
func TestProposerBlocksAreBuiltOnPreferredProBlock(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// add two proBlks...
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
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
		ParentV:    coreGenBlk,
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	// ...and set one as preferred
	var prefcoreBlk *snowman.TestBlock
	coreVM.CantSetPreference = true
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
	coreVM.CantParseBlock = true
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
		ParentV:    prefcoreBlk,
		HeightV:    prefcoreBlk.Height() + 1,
		TimestampV: prefcoreBlk.Timestamp().Add(2 * proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build block")
	}

	// ...show that parent is the preferred one
	if builtBlk.Parent().ID() != proBlk2.ID() {
		t.Fatal("proposer block not built on preferred parent")
	}
}

func TestCoreBlocksMustBeBuiltOnPreferredCoreBlock(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
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
		ParentV:    coreGenBlk,
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}

	// ...and set one as preferred
	var wronglyPreferredcoreBlk *snowman.TestBlock
	coreVM.CantSetPreference = true
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
	coreVM.CantParseBlock = true
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
		ParentV:    wronglyPreferredcoreBlk,
		HeightV:    wronglyPreferredcoreBlk.Height() + 1,
		TimestampV: wronglyPreferredcoreBlk.Timestamp().Add(2 * proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }

	blk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != ErrProBlkWrongParent {
		t.Fatal("coreVM does not build on preferred coreBlock. It should err")
	}
}

// VM.ParseBlock tests section
// func TestParseBlockRecordsButDoesNotVerifyParsedBlock(t *testing.T) {
// 	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
// 	pChainHeight := uint64(100)
// 	valVM.CantGetCurrentHeight = true
// 	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(222),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{2},
// 		ParentV:    coreGenBlk,
// 		HeightV:    coreGenBlk.Height() + 1,
// 		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
// 	}
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

// 	slb, err := statelessblock.Build(
// 		proVM.preferred,
// 		coreGenBlk.Timestamp(),
// 		pChainHeight,
// 		proVM.stakingCert.Leaf,
// 		coreGenBlk.Bytes(),
// 		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk := ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreGenBlk,
// 		status:  choices.Processing,
// 	}
// 	// test
// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.Bytes()):
// 			return coreGenBlk, nil
// 		default:
// 			t.Fatalf("Wrong bytes")
// 			return nil, nil
// 		}
// 	}

// 	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
// 	if err != nil {
// 		t.Fatal("proposerVM could not parse block")
// 	}
// 	if err := parsedBlk.Verify(); err == nil {
// 		t.Fatal("parsed block should not necessarily verify upon parse")
// 	}

// 	// test
// 	coreVM.CantGetBlock = false // forbid calls to coreVM to show caching
// 	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
// 	if err != nil {
// 		t.Fatal("proposerVM has not cached parsed block")
// 	}
// 	if storedBlk != parsedBlk {
// 		t.Fatal("proposerVM retrieved wrong block")
// 	}
// }

func TestTwoProBlocksWrappingSameCoreBlockCanBeParsed(t *testing.T) {
	coreVM, _, proVM, gencoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// create two Proposer blocks at the same height
	coreBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		ParentV:    gencoreBlk,
		HeightV:    gencoreBlk.Height() + 1,
		TimestampV: proVM.Time(),
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	slb1, err := statelessblock.Build(
		proVM.preferred,
		coreBlk.Timestamp(),
		100, // pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		coreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk1 := ProposerBlock{
		Block:   slb1,
		vm:      proVM,
		coreBlk: coreBlk,
		status:  choices.Processing,
	}

	slb2, err := statelessblock.Build(
		proVM.preferred,
		coreBlk.Timestamp(),
		200, // pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		coreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk2 := ProposerBlock{
		Block:   slb2,
		vm:      proVM,
		coreBlk: coreBlk,
		status:  choices.Processing,
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

	rtrvdProBlk1, err := proVM.GetBlock(proBlk1.ID())
	if err != nil {
		t.Fatal("Could not retrieve proBlk1")
	}
	if rtrvdProBlk1.ID() != proBlk1.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}

	rtrvdProBlk2, err := proVM.GetBlock(proBlk2.ID())
	if err != nil {
		t.Fatal("Could not retrieve proBlk1")
	}
	if rtrvdProBlk2.ID() != proBlk2.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
}

// VM.BuildBlock and VM.ParseBlock interoperability tests section
func TestTwoProBlocksWithSameParentCanBothVerify(t *testing.T) {
	coreVM, _, proVM, gencoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// one block is built from this proVM
	localcoreBlk := &snowman.TestBlock{
		BytesV:     []byte{111},
		ParentV:    gencoreBlk,
		HeightV:    gencoreBlk.Height() + 1,
		TimestampV: genesisTimestamp,
	}
	coreVM.CantBuildBlock = true
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
		ParentV:    gencoreBlk,
		HeightV:    gencoreBlk.Height() + 1,
		TimestampV: genesisTimestamp,
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, netcoreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return netcoreBlk, nil
	}

	pChainHeight, err := proVM.PChainHeight()
	if err != nil {
		t.Fatal("could not retrieve pChain height")
	}

	netSlb, err := statelessblock.Build(
		proVM.preferred,
		netcoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		netcoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	netProBlk := ProposerBlock{
		Block:   netSlb,
		vm:      proVM,
		coreBlk: netcoreBlk,
		status:  choices.Processing,
	}

	// prove that also block from network verifies
	if err = netProBlk.Verify(); err != nil {
		t.Fatal("block from network does not verify")
	}
}

// // VM persistency test section
// func TestProposerVMCacheCanBeRebuiltFromDB(t *testing.T) {
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

// 	// build two blocks on top of genesis
// 	coreBlk1 := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV: ids.GenerateTestID(),
// 		},
// 		ParentV: coreGenBlk,
// 		BytesV:  []byte{1},
// 		VerifyV: nil,
// 	}
// 	coreBlk2 := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV: ids.GenerateTestID(),
// 		},
// 		ParentV: coreBlk1,
// 		BytesV:  []byte{2},
// 		VerifyV: nil,
// 	}
// 	coreBuildCalls := 0
// 	coreVM.BuildBlockF = func() (snowman.Block, error) {
// 		switch coreBuildCalls {
// 		case 0:
// 			coreBuildCalls++
// 			return coreBlk1, nil
// 		case 1:
// 			coreBuildCalls++
// 			return coreBlk2, nil
// 		default:
// 			t.Fatal("BuildBlock of coreVM called too many times")
// 		}
// 		return nil, nil
// 	}
// 	proBlk1, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("Could not build block")
// 	}

// 	// update preference to build next block
// 	if err := proVM.SetPreference(proBlk1.ID()); err != nil {
// 		t.Fatal("Could not set preference")
// 	}

// 	proBlk2, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("Could not build block")
// 	}

// 	// while inner cache, as it would happen upon node shutdown
// 	proVM.state.wipeCache()

// 	// build a new block to show ops can resume smoothly
// 	coreBlk3 := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV: ids.GenerateTestID(),
// 		},
// 		ParentV: coreBlk2,
// 		BytesV:  []byte{3},
// 		VerifyV: nil,
// 	}
// 	coreVM.BuildBlockF = func() (snowman.Block, error) {
// 		return coreBlk3, nil
// 	}
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.BytesV):
// 			return coreGenBlk, nil
// 		case bytes.Equal(b, coreBlk1.BytesV):
// 			return coreBlk1, nil
// 		case bytes.Equal(b, coreBlk2.BytesV):
// 			return coreBlk2, nil
// 		default:
// 			t.Fatal("ParseBlock of coreVM called with unknown block")
// 		}
// 		return nil, nil
// 	}

// 	// update preference to build next block
// 	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
// 		t.Fatal("Could not set preference")
// 	}

// 	proBlk3, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("Could not build block")
// 	}

// 	if proBlk3.Parent().ID() != proBlk2.ID() {
// 		t.Fatal("Error in building Block")
// 	}

// 	// while inner cache, as it would happen upon node shutdown
// 	proVM.state.wipeCache()

// 	// check that getBlock still works on older blocks
// 	rtrvdProBlk2, err := proVM.GetBlock(proBlk2.ID())
// 	if err != nil {
// 		t.Fatal("Could not get block after whiping off proposerVM cache")
// 	}
// 	if rtrvdProBlk2.ID() != proBlk2.ID() {
// 		t.Fatal("blocks do not match following cache whiping")
// 	}
// 	if err = rtrvdProBlk2.Verify(); err != nil {
// 		t.Fatal("block retrieved after cache whiping does not verify")
// 	}

// 	rtrvdProBlk1, err := proVM.GetBlock(proBlk1.ID())
// 	if err != nil {
// 		t.Fatal("Could not get block after whiping off proposerVM cache")
// 	}
// 	if rtrvdProBlk1.ID() != proBlk1.ID() {
// 		t.Fatal("blocks do not match following cache whiping")
// 	}
// 	if err = rtrvdProBlk1.Verify(); err != nil {
// 		t.Fatal("block retrieved after cache whiping does not verify")
// 	}
// }

// VM backward compatibility tests section
func TestPreSnowmanInitialize(t *testing.T) {
	_, _, proVM, genesisBlk := initTestProposerVM(t, timer.MaxTime) // disable ProBlks

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	rtvdBlk, err := proVM.GetBlock(blkID)
	if err != nil {
		t.Fatal("Block should be returned without calling core vm")
	}

	if _, ok := rtvdBlk.(*ProposerBlock); ok {
		t.Fatal("retrieved block should not be a proposer block")
	}

	if !bytes.Equal(rtvdBlk.Bytes(), genesisBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

// func TestPreSnowmanBuildBlock(t *testing.T) {
// 	// setup
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(333),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{3},
// 		ParentV:    coreGenBlk,
// 		HeightV:    coreGenBlk.Height() + 1,
// 		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

// 	// test
// 	builtBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("proposerVM could not build block")
// 	}
// 	if builtBlk.ID() != coreBlk.ID() {
// 		t.Fatal("unexpected built block")
// 	}

// 	// test
// 	coreVM.CantGetBlock = true
// 	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) { return coreBlk, nil }
// 	storedBlk, err := proVM.GetBlock(builtBlk.ID())
// 	if err != nil {
// 		t.Fatal("proposerVM has not cached built block")
// 	}
// 	if storedBlk != builtBlk {
// 		t.Fatal("proposerVM retrieved wrong block")
// 	}
// }

// func TestPreSnowmanParseBlock(t *testing.T) {
// 	// setup
// 	coreVM, _, proVM, _ := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV: ids.Empty.Prefix(2021),
// 		},
// 		BytesV: []byte{0},
// 	}

// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		if !bytes.Equal(b, coreBlk.Bytes()) {
// 			t.Fatalf("Wrong bytes")
// 		}
// 		return coreBlk, nil
// 	}

// 	parsedBlk, err := proVM.ParseBlock(coreBlk.Bytes())
// 	if err != nil {
// 		t.Fatal("Could not parse naked core block")
// 	}
// 	if parsedBlk.ID() != coreBlk.ID() {
// 		t.Fatal("Parsed block does not match expected block")
// 	}

// 	coreVM.CantGetBlock = true
// 	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
// 		if id != coreBlk.ID() {
// 			t.Fatalf("Unknown core block")
// 		}
// 		return coreBlk, nil
// 	}
// 	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
// 	if err != nil {
// 		t.Fatal("proposerVM has not cached parsed block")
// 	}
// 	if storedBlk != parsedBlk {
// 		t.Fatal("proposerVM retrieved wrong block")
// 	}
// }

// func TestPreSnowmanSetPreference(t *testing.T) {
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(333),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{3},
// 		ParentV:    coreGenBlk,
// 		HeightV:    coreGenBlk.Height() + 1,
// 		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
// 	builtBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("Could not build proposer block")
// 	}

// 	// test
// 	if err = proVM.SetPreference(builtBlk.ID()); err != nil {
// 		t.Fatal("Could not set preference on proposer Block")
// 	}
// }

// func TestPreSnowmanBlockAccept(t *testing.T) {
// 	coreVM, _, proVM, _ := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(10),
// 			StatusV: choices.Accepted,
// 		},
// 	}

// 	coreVM.CantLastAccepted = true
// 	coreVM.LastAcceptedF = func() (ids.ID, error) {
// 		return coreBlk.ID(), nil
// 	}

// 	// test
// 	laID, err := proVM.LastAccepted()
// 	if err != nil {
// 		t.Fatal("Could not retrieve last accepted proposer Block ID")
// 	}
// 	if laID != coreBlk.ID() {
// 		t.Fatal("Unexpected last accepted proposer block ID")
// 	}
// }
