package proposervm

import (
	"bytes"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/staking"
)

var pTestCert *tls.Certificate = nil // package variable to init it only once

type TestConnectorProVM struct {
	block.TestVM
}

func (vm *TestConnectorProVM) Connected(validatorID ids.ShortID) (bool, error) {
	return false, nil
}

func (vm *TestConnectorProVM) Disconnected(validatorID ids.ShortID) (bool, error) {
	return true, nil
}

func TestProposerVMConnectorHandling(t *testing.T) {
	// setup
	noConnectorVM := &block.TestVM{}
	proVM := VM{
		ChainVM: noConnectorVM,
	}

	// test
	_, err := proVM.Connected(ids.ShortID{})
	if err != ErrInnerVMNotConnector {
		t.Fatal("Proposer VM should signal that it wraps a ChainVM not implementing Connector interface with ErrInnerVMNotConnector error")
	}

	_, err = proVM.Disconnected(ids.ShortID{})
	if err != ErrInnerVMNotConnector {
		t.Fatal("Proposer VM should signal that it wraps a ChainVM not implementing Connector interface with ErrInnerVMNotConnector error")
	}

	// setup
	proConnVM := TestConnectorProVM{
		TestVM: block.TestVM{},
	}

	// test
	_, err = proConnVM.Connected(ids.ShortID{})
	if err != nil {
		t.Fatal("Proposer VM should forward wrapped Connection state if this implements Connector interface")
	}

	_, err = proConnVM.Disconnected(ids.ShortID{})
	if err != nil {
		t.Fatal("Proposer VM should forward wrapped Disconnection state if this implements Connector interface")
	}
}

func TestInitializeRecordsGenesis(t *testing.T) {
	ChainVM, proVM, genesisBlk := initTestProposerVM(t, true)

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	ChainVM.CantGetBlock = false
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
	_, proVM, genesisBlk := initTestProposerVM(t, false)

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

///////////////////////////////////////////////////////////////////////////////
///////////////////////////////////////////////////////////////////////////////
func initTestProposerVM(t *testing.T, useProHdr bool) (*block.TestVM, *VM, *snowman.TestBlock) {
	// setup
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV:  []byte{0},
		HeightV: 0,
	}

	ChainVM := &block.TestVM{}
	ChainVM.CantInitialize = true
	ChainVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	ChainVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	ChainVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return coreGenBlk, nil }

	tc := &testClock{
		setTime: time.Now(),
	}
	proVM := NewProVM(ChainVM, useProHdr)
	proVM.clk = tc

	if pTestCert == nil {
		var err error
		pTestCert, _ = staking.NewTLSCert()
		if err != nil {
			t.Fatal("Could not generate dummy StakerCert")
		}
	}

	dummyCtx := &snow.Context{
		StakingKey: &pTestCert.PrivateKey,
	}
	dummyDBManager := manager.NewDefaultMemDBManager()
	if err := proVM.Initialize(dummyCtx, dummyDBManager, coreGenBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}
	return ChainVM, &proVM, coreGenBlk
}

func TestBuildBlockRecordsAndVerifiesBuiltBlock(t *testing.T) {
	// setup
	ChainVM, proVM, coreGenBlk := initTestProposerVM(t, true)
	ChainVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.HeightV + 1,
		VerifyV: nil,
	}
	ChainVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("built block should be verified")
	}

	// test
	ChainVM.CantGetBlock = false // forbid calls to ChainVM to show caching
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
	ChainVM, proVM, genesisBlk := initTestProposerVM(t, true)

	newBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: genesisBlk,
		HeightV: 1,
		BytesV:  []byte{1},
	}
	ChainVM.BuildBlockF = func() (snowman.Block, error) { return newBlk, nil }

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
	ChainVM, proVM, _ := initTestProposerVM(t, true)

	coreBlkDoesNotVerify := errors.New("coreBlk should not verify in this test")
	coreBlk := &snowman.TestBlock{
		BytesV:  []byte{1},
		VerifyV: coreBlkDoesNotVerify,
	}
	ChainVM.CantParseBlock = true
	ChainVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr := NewProHeader(proGenBlkID, time.Now().AddDate(0, 0, -1).Unix(), coreBlk.Height())
	proBlk, _ := NewProBlock(proVM, proHdr, coreBlk, nil, false) // not signing block, cannot err

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse block")
	}
	if err := parsedBlk.Verify(); err == nil {
		t.Fatal("parsed block should not necessarily verify upon parse")
	}

	// test
	ChainVM.CantGetBlock = false // forbid calls to ChainVM to show caching
	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached parsed block")
	}
	if storedBlk != parsedBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestProposerVMCacheCanBeRebuiltFromDB(t *testing.T) {
	ChainVM, proVM, coreGenBlk := initTestProposerVM(t, true)

	// build two blocks on top of genesis
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreGenBlk,
		HeightV: 1,
		BytesV:  []byte{1},
		VerifyV: nil,
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreBlk1,
		HeightV: 2,
		BytesV:  []byte{2},
		VerifyV: nil,
	}
	coreBuildCalls := 0
	ChainVM.BuildBlockF = func() (snowman.Block, error) {
		switch coreBuildCalls {
		case 0:
			coreBuildCalls++
			return coreBlk1, nil
		case 1:
			coreBuildCalls++
			return coreBlk2, nil
		default:
			t.Fatal("BuildBlock of ChainVM called too many times")
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
		HeightV: 3,
		BytesV:  []byte{3},
		VerifyV: nil,
	}
	ChainVM.BuildBlockF = func() (snowman.Block, error) {
		return coreBlk3, nil
	}
	ChainVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.BytesV):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.BytesV):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.BytesV):
			return coreBlk2, nil
		default:
			t.Fatal("ParseBlock of ChainVM called with unknown block")
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
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV:  []byte{0},
		HeightV: 0,
	}

	ChainVM := &block.TestVM{}
	ChainVM.CantParseBlock = true
	ChainVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proVM := NewProVM(ChainVM, true) // TODO: FIX AND USE ordinary INIT
	proVM.state.init(memdb.New())

	parsedBlk, err := proVM.ParseBlock(coreBlk.Bytes())
	if err != nil {
		t.Fatal("Could not parse naked core block")
	}
	if parsedBlk.ID() != coreBlk.ID() {
		t.Fatal("Parsed block does not match expected block")
	}

	ChainVM.CantGetBlock = true
	ChainVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
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
	ChainVM, proVM, coreGenBlk := initTestProposerVM(t, true)

	ChainVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(10),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.HeightV + 1,
		VerifyV: nil,
	}
	ChainVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// test
	if err = proVM.SetPreference(proBlk.ID()); err != nil {
		t.Fatal("Could not set preference on proposer Block")
	}

	ChainVM.CantGetBlock = true
	nakedCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(999),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.HeightV + 1,
		VerifyV: nil,
	}
	ChainVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
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
	ChainVM, proVM, _ := initTestProposerVM(t, true)

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(10),
			StatusV: choices.Accepted,
		},
	}

	ChainVM.CantLastAccepted = true
	ChainVM.LastAcceptedF = func() (ids.ID, error) {
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
	ChainVM, proVM, coreGenBlk := initTestProposerVM(t, false)
	ChainVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.HeightV + 1,
		VerifyV: nil,
	}
	ChainVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("built block should be verified")
	}

	// test
	ChainVM.CantGetBlock = true
	ChainVM.GetBlockF = func(id ids.ID) (snowman.Block, error) { return coreBlk, nil }
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk != builtBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}
