package proposervm

import (
	"bytes"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

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
	coreVM, proVM, genesisBlk := initTestProposerVM(t)

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

	if !bytes.Equal(proRtvdBlk.Block.Bytes(), genesisBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

///////////////////////////////////////////////////////////////////////////////
///////////////////////////////////////////////////////////////////////////////
func initTestProposerVM(t *testing.T) (*block.TestVM, VM, *snowman.TestBlock) {
	// setup
	coreGenesisBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV:  []byte{1},
		HeightV: 0,
	}

	coreVM := &block.TestVM{}
	coreVM.CantInitialize = true
	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenesisBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return coreGenesisBlk, nil }

	tc := &testClock{
		setTime: time.Now(),
	}
	proVM := NewProVM(coreVM)
	proVM.clk = tc

	if err := proVM.Initialize(nil, nil, coreGenesisBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}
	return coreVM, proVM, coreGenesisBlk
}

func TestBuildBlockRecordsAndVerifiesBuiltBlock(t *testing.T) {
	// setup
	coreVM, proVM, coreGenBlk := initTestProposerVM(t)
	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.HeightV + 1,
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
	coreVM, proVM, genesisBlk := initTestProposerVM(t)

	newBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: genesisBlk,
		HeightV: 1,
		BytesV:  []byte{1},
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

	if proBlock.Block != newBlk {
		t.Fatal("different block was expected to be built")
	}

	if proBlock.Parent().ID() == genesisBlk.ID() {
		t.Fatal("first block not built on genesis")
	}
}

func TestParseBlockRecordsAndVerifiesParsedBlock(t *testing.T) {
	coreVM, proVM, coreGenBlk := initTestProposerVM(t)

	coreBlk := &snowman.TestBlock{
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr := ProposerBlockHeader{
		PrntID:    proGenBlkID,
		Timestamp: time.Now().AddDate(0, 0, -1).Unix(),
		Height:    coreBlk.Height(),
	}
	proBlk := NewProBlock(&proVM, proHdr, coreBlk, nil)

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse block")
	}
	if err := parsedBlk.Verify(); err != nil {
		t.Fatal("parsed block should be verified")
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
