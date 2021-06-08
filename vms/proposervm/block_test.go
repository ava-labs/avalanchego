package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type TestOptionsBlock struct {
	snowman.TestBlock
}

func (tob TestOptionsBlock) Options() ([2]snowman.Block, error) {
	return [2]snowman.Block{}, nil
}

func TestProposerBlockOptionsHandling(t *testing.T) {
	// setup
	noOptionBlock := snowman.TestBlock{}
	proBlk := ProposerBlock{
		Block: &noOptionBlock,
	}

	// test
	_, err := proBlk.Options()
	if err != ErrInnerBlockNotOracle {
		t.Fatal("Proposer block should signal that it wraps a block not implementing Options interface with ErrNotOracleBlock error")
	}

	// setup
	proBlk = ProposerBlock{
		Block: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options()
	if err != nil {
		t.Fatal("Proposer block should forward wrapped block options if this implements Option interface")
	}
}

///////////////////////////////////////////////////////////////////////////////
///////////////////////////////////////////////////////////////////////////////

type testClock struct {
	setTime time.Time
}

func (tC testClock) now() time.Time {
	return tC.setTime
}

func TestProposerBlockHeaderIsMarshalled(t *testing.T) {
	// setup
	coreVM, proVM, genesisBlk := initTestProposerVM(t)

	newBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: genesisBlk,
		HeightV: genesisBlk.HeightV + 1,
		BytesV:  []byte{1},
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return newBlk, nil }

	proBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build proposer block")
	}

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, newBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return newBlk, nil
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
		Timestamp: proVM.clk.now().Unix(),
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
	proBlk, _ := NewProBlock(&proVM, proHdr, coreBlk, nil, false) // not signing block, cannot err

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

func TestProposerBlockWithUnknownParentDoesNotVerify(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM)
	proVM.state.init(memdb.New())

	ParentProBlk, _ := NewProBlock(&proVM, ProposerBlockHeader{}, &snowman.TestBlock{}, nil,
		false) // not signing block, cannot err

	childHdr := ProposerBlockHeader{
		PrntID: ParentProBlk.ID(),
		Height: ParentProBlk.Height() + 1,
	}
	childCoreBlk := &snowman.TestBlock{
		VerifyV: nil,
		HeightV: childHdr.Height,
	}
	childProBlk, _ := NewProBlock(&proVM, childHdr, childCoreBlk, nil, false) // not signing block, cannot err

	// Parent block not store yet
	err := childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	} else if err != ErrProBlkNotFound {
		t.Fatal("Block with unknown parent should have different error")
	}

	// now store parentBlock
	proVM.state.cacheProBlk(&ParentProBlk)

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Block with known parent should not verify")
	}
}

func TestProposerBlockOlderThanItsParentDoesNotVerify(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM)

	parentHdr := ProposerBlockHeader{
		Timestamp: proVM.clk.now().Unix(),
	}
	ParentProBlk, _ := NewProBlock(&proVM, parentHdr, &snowman.TestBlock{}, nil, false) // not signing block, cannot err
	proVM.state.cacheProBlk(&ParentProBlk)

	childHdr := ProposerBlockHeader{
		PrntID: ParentProBlk.ID(),
		Height: ParentProBlk.Height() + 1,
	}
	childCoreBlk := &snowman.TestBlock{
		VerifyV: nil,
		HeightV: childHdr.Height,
	}
	childProBlk, _ := NewProBlock(&proVM, childHdr, childCoreBlk, nil, false) // not signing block, cannot err

	childProBlk.header.Timestamp = time.Unix(ParentProBlk.header.Timestamp, 0).Add(-1 * time.Second).Unix()
	err := childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Old proposer block timestamp should have different error")
	}

	childProBlk.header.Timestamp = ParentProBlk.header.Timestamp
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp equal to parent block timestamp should verify")
	}

	childProBlk.header.Timestamp = time.Unix(ParentProBlk.header.Timestamp, 0).Add(BlkSubmissionTolerance).Unix()
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	childProBlk.header.Timestamp = time.Unix(ParentProBlk.header.Timestamp, 0).Add(BlkSubmissionTolerance + time.Second).Unix()
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestProposerBlockWithWrongHeightDoesNotVerify(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM)

	ParentProBlk, _ := NewProBlock(&proVM,
		ProposerBlockHeader{
			Height: 200,
		},
		&snowman.TestBlock{
			HeightV: 200,
		}, nil, false) // not signing block, cannot err
	proVM.state.cacheProBlk(&ParentProBlk)

	childHdr := ProposerBlockHeader{
		PrntID: ParentProBlk.ID(),
	}
	childCoreBlk := &snowman.TestBlock{
		VerifyV: nil,
		HeightV: ParentProBlk.Height() + 1,
	}
	childProBlk, _ := NewProBlock(&proVM, childHdr, childCoreBlk, nil, false)

	// child block must strictly follow parent block height
	childProBlk.header.Height = ParentProBlk.Height() - 1
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block has wrong height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	childProBlk.header.Height = ParentProBlk.Height()
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block has wrong height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	childProBlk.header.Height = ParentProBlk.Height() + 1
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block height should follow parent height")
	}

	childProBlk.header.Height = ParentProBlk.Height() + 2
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block has wrong height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}
}
