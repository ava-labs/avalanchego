package proposervm

import (
	"bytes"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/staking"
)

var pTestCert *tls.Certificate = nil // package variable to init it only once

func TestInitializeRecordsGenesis(t *testing.T) {
	coreVM, proVM, genesisBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	coreVM.CantGetBlock = false
	rtvdBlk, err := proVM.GetBlock(blkID)
	if err != nil {
		t.Fatal("Block should be returned without calling core vm")
	}

	proRtvdBlk, ok := rtvdBlk.(*ProposerBlock)
	if !ok {
		t.Fatal("retrieved block is not a proposer block")
	}

	if !bytes.Equal(proRtvdBlk.coreBlk.Bytes(), genesisBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

func TestInitializeCanRecordsCoreGenesis(t *testing.T) {
	_, proVM, genesisBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

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

func initTestProposerVM(t *testing.T, proBlkStartTime time.Time) (*block.TestVM, *VM, *snowman.TestBlock) {
	// setup
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV:  []byte{0},
		HeightV: 0,
	}

	coreVM := &block.TestVM{}
	coreVM.CantInitialize = true
	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return coreGenBlk, nil }

	tc := &testClock{
		setTime: time.Now(),
	}
	proVM := NewProVM(coreVM, proBlkStartTime)
	proVM.clock = tc

	if pTestCert == nil {
		var err error
		pTestCert, err = staking.NewTLSCert()
		if err != nil {
			t.Fatal("Could not generate dummy StakerCert")
		}
	}

	dummyCtx := &snow.Context{
		StakingCert: *pTestCert,
	}
	dummyDBManager := manager.NewDefaultMemDBManager()
	if err := proVM.Initialize(dummyCtx, dummyDBManager, coreGenBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}
	return coreVM, &proVM, coreGenBlk
}

func TestBuildBlockRecordsAndVerifiesBuiltBlock(t *testing.T) {
	// setup
	coreVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("built block should be verified")
	}

	// test
	coreVM.CantGetBlock = false // forbid calls to coreVM to show caching
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk != builtBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	// setup
	coreVM, proVM, genesisBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	newBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		ParentV: genesisBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return newBlk, nil }

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

	if proBlock.coreBlk != newBlk {
		t.Fatal("different block was expected to be built")
	}

	if proBlock.Parent().ID() == genesisBlk.ID() {
		t.Fatal("first block not built on genesis")
	}
}

func TestParseBlockRecordsButDoesNotVerifyParsedBlock(t *testing.T) {
	coreVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlkDoesNotVerify := errors.New("coreBlk should not verify in this test")
	coreBlk := &snowman.TestBlock{
		BytesV:  []byte{1},
		VerifyV: coreBlkDoesNotVerify,
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr := NewProHeader(proGenBlkID, time.Now().AddDate(0, 0, -1).Unix(), coreBlk.Height(), *pTestCert.Leaf)
	proBlk, err := NewProBlock(proVM, proHdr, coreBlk, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse block")
	}
	if err := parsedBlk.Verify(); err == nil {
		t.Fatal("parsed block should not necessarily verify upon parse")
	}

	// test
	coreVM.CantGetBlock = false // forbid calls to coreVM to show caching
	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached parsed block")
	}
	if storedBlk != parsedBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestProposerVMCacheCanBeRebuiltFromDB(t *testing.T) {
	coreVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// build two blocks on top of genesis
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreGenBlk,
		BytesV:  []byte{1},
		VerifyV: nil,
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreBlk1,
		BytesV:  []byte{2},
		VerifyV: nil,
	}
	coreBuildCalls := 0
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		switch coreBuildCalls {
		case 0:
			coreBuildCalls++
			return coreBlk1, nil
		case 1:
			coreBuildCalls++
			return coreBlk2, nil
		default:
			t.Fatal("BuildBlock of coreVM called too many times")
		}
		return nil, nil
	}
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	// while inner cache, as it would happen upon node shutdown
	proVM.state.wipeCache()

	// build a new block to show ops can resume smoothly
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreBlk2,
		BytesV:  []byte{3},
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		return coreBlk3, nil
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.BytesV):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.BytesV):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.BytesV):
			return coreBlk2, nil
		default:
			t.Fatal("ParseBlock of coreVM called with unknown block")
		}
		return nil, nil
	}

	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	if proBlk3.Parent().ID() != proBlk2.ID() {
		t.Fatal("Error in building Block")
	}

	// check that getBlock still works on older blocks
	rtrvdProBlk2, err := proVM.GetBlock(proBlk2.ID())
	if err != nil {
		t.Fatal("Could not get block after whiping off proposerVM cache")
	}
	if rtrvdProBlk2.ID() != proBlk2.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
	if err = rtrvdProBlk2.Verify(); err != nil {
		t.Fatal("block retrieved after cache whiping does not verify")
	}

	rtrvdProBlk1, err := proVM.GetBlock(proBlk1.ID())
	if err != nil {
		t.Fatal("Could not get block after whiping off proposerVM cache")
	}
	if rtrvdProBlk1.ID() != proBlk1.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
	if err = rtrvdProBlk1.Verify(); err != nil {
		t.Fatal("block retrieved after cache whiping does not verify")
	}
}

func TestProposerVMCanParseAndCacheCoreBlocksWithNoHeader(t *testing.T) {
	// setup
	coreVM, proVM, _ := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		BytesV: []byte{0},
	}

	coreVM.CantParseBlock = true
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
	if parsedBlk.ID() != coreBlk.ID() {
		t.Fatal("Parsed block does not match expected block")
	}

	coreVM.CantGetBlock = true
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
	if storedBlk != parsedBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestSetPreferenceWorksWithProBlocksAndWrappedBlocks(t *testing.T) {
	coreVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(10),
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// test
	if err = proVM.SetPreference(proBlk.ID()); err != nil {
		t.Fatal("Could not set preference on proposer Block")
	}

	coreVM.CantGetBlock = true
	nakedCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(999),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id == nakedCoreBlk.ID() {
			return nakedCoreBlk, nil
		}
		t.Fatal("Unexpected get call")
		return nil, nil
	}

	// test
	if err = proVM.SetPreference(nakedCoreBlk.ID()); err != nil {
		t.Fatal("Could not set preference on naked core block")
	}
}

func TestLastAcceptedWorksWithProBlocksAndWrappedBlocks(t *testing.T) {
	coreVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(10),
			StatusV: choices.Accepted,
		},
	}

	coreVM.CantLastAccepted = true
	coreVM.LastAcceptedF = func() (ids.ID, error) {
		return coreBlk.ID(), nil
	}

	proHdr := ProposerBlockHeader{}
	proBlk, _ := NewProBlock(proVM, proHdr, coreBlk, nil, false) // not signing block, cannot err
	proVM.state.cacheProBlk(&proBlk)

	// test
	laID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("Could not retrieve last accepted proposer Block ID")
	}
	if laID != proBlk.ID() {
		t.Fatal("Unexpected last accepted proposer block ID")
	}

	proVM.state.wipeCache() // wipe cache so that only coreBlk will remain

	// test
	laID, err = proVM.LastAccepted()
	if err != nil {
		t.Fatal("Could not retrieve last accepted core Block ID")
	}
	if laID != coreBlk.ID() {
		t.Fatal("Unexpected last accepted proposer block ID")
	}
}

func TestBuildBlockCanReturnCoreBlocks(t *testing.T) {
	// setup
	coreVM, proVM, coreGenBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks
	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("built block should be verified")
	}

	// test
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) { return coreBlk, nil }
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk != builtBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}
