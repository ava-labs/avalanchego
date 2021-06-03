package proposervm

import (
	"bytes"
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
)

func TestProposerVmInitializeRecordsGenesis(t *testing.T) {
	// setup
	genesisBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV: []byte{1},
	}

	coreVM := &block.TestVM{}
	coreVM.CantInitialize = true
	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return genesisBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return genesisBlk, nil }

	proVM := NewProVM(coreVM)

	// test
	if err := proVM.Initialize(nil, nil, genesisBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	rtvdBlk, err := proVM.GetBlock(blkID)
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	proRtvdBlk, ok := rtvdBlk.(*ProposerBlock)
	if !ok {
		t.Fatal("retrieved block is not a proposer block")
	}

	if !bytes.Equal(proRtvdBlk.Block.Bytes(), genesisBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	// setup
	genesisBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV: []byte{1},
	}

	coreVM := &block.TestVM{}
	coreVM.CantInitialize = true
	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return genesisBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return genesisBlk, nil }

	proVM := NewProVM(coreVM)
	if err := proVM.Initialize(nil, nil, genesisBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}

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

func TestProposerBlockHeaderIsMarshalled(t *testing.T) {
	// setup
	genesisBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Unknown,
		},
		BytesV: []byte{1},
	}

	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM)

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

	proHdr := ProposerBlockHeader{Timestamp: time.Now().AddDate(0, 0, -1).Unix()}
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: genesisBlk,
		HeightV: 1,
		BytesV:  []byte{1},
	}
	proBlk := NewProBlock(&proVM, proHdr, coreBlk)

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	// test
	rcvdBlk, errRcvd := proVM.ParseBlock(proBlk.Bytes())
	if errRcvd != nil {
		t.Fatal("failed parsing proposervm.Block. Error:", errRcvd)
	}

	if rcvdBlk.ID() != proBlk.ID() {
		t.Fatal("Parsed proposerBlock is different than original one")
	}
}

func TestProposerBlockParseFailure(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM)

	proHdr := ProposerBlockHeader{
		PrntID:    ids.Empty.Prefix(8),
		Timestamp: time.Now().Unix(),
	}
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: nil, // could be genesis
		HeightV: 1,
		BytesV:  []byte{1},
	}
	proBlk := NewProBlock(&proVM, proHdr, coreBlk)

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		return coreBlk, errors.New("Block marshalling failed")
	}

	// test
	rcvdBlk, errRcvd := proVM.ParseBlock(proBlk.Bytes())
	if errRcvd == nil {
		t.Fatal("failed parsing proposervm.Block. Error:", errRcvd)
	}

	if rcvdBlk != nil {
		t.Fatal("upon failure proposervm.VM.ParseBlock should return nil snowman.Block")
	}
}
