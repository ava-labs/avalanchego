package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
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
		coreBlk: &noOptionBlock,
	}

	// test
	_, err := proBlk.Options()
	if err != ErrInnerBlockNotOracle {
		t.Fatal("Proposer block should signal that it wraps a block not implementing Options interface with ErrNotOracleBlock error")
	}

	// setup
	proBlk = ProposerBlock{
		coreBlk: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options()
	if err != nil {
		t.Fatal("Proposer block should forward wrapped block options if this implements Option interface")
	}
}

type testClock struct {
	setTime time.Time
}

func (tC testClock) now() time.Time {
	return tC.setTime
}

func TestProposerBlockHeaderIsMarshalled(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM, time.Unix(0, 0)) // enable ProBlks
	proVM.state.init(memdb.New())

	proHdr := NewProHeader(ids.Empty.Prefix(8), proVM.now().Unix(), 100)
	newBlk := &snowman.TestBlock{
		BytesV: []byte{1},
	}
	proBlk, _ := NewProBlock(&proVM, proHdr, newBlk, nil, false) // not signing block, cannot err

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
	proVM := NewProVM(coreVM, time.Unix(0, 0)) // enable ProBlks

	proHdr := NewProHeader(ids.Empty.Prefix(8), proVM.now().Unix(), 0)
	proBlk, _ := NewProBlock(&proVM, proHdr, &snowman.TestBlock{}, nil, false) // not signing block, cannot err

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		return nil, errors.New("Block marshalling failed")
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

func TestProposerBlockVerificationParent(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM, time.Unix(0, 0)) // enable ProBlks
	proVM.state.init(memdb.New())

	prntProHdr := NewProHeader(ids.ID{}, 0, proVM.pChainHeight())
	prntProBlk, _ := NewProBlock(&proVM, prntProHdr, &snowman.TestBlock{},
		nil, false) // not signing block, cannot err

	childProHdr := NewProHeader(prntProBlk.ID(), 0, proVM.pChainHeight())
	childProBlk, _ := NewProBlock(&proVM, childProHdr, &snowman.TestBlock{
		VerifyV: nil,
	}, nil, false) // not signing block, cannot err

	// Parent block not store yet
	err := childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	} else if err != ErrProBlkNotFound {
		t.Fatal("Block with unknown parent should have different error")
	}

	// now store parentBlock
	proVM.state.cacheProBlk(&prntProBlk)

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Block with known parent should verify")
	}
}

func TestProposerBlockVerificationTimestamp(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM, time.Unix(0, 0)) // enable ProBlks

	prntTimestamp := proVM.now().Unix()
	ParentProBlk, _ := NewProBlock(&proVM,
		NewProHeader(ids.ID{}, prntTimestamp, proVM.pChainHeight()),
		&snowman.TestBlock{},
		nil, false) // not signing block, cannot err
	proVM.state.cacheProBlk(&ParentProBlk)

	childProBlk, _ := NewProBlock(&proVM,
		NewProHeader(ParentProBlk.ID(), 0, proVM.pChainHeight()),
		&snowman.TestBlock{
			VerifyV: nil,
		},
		nil, false) // not signing block, cannot err

	// child block timestamp cannot be lower than parent timestamp
	childProBlk.header.Timestamp = time.Unix(prntTimestamp, 0).Add(-1 * time.Second).Unix()
	err := childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Old proposer block timestamp should have different error")
	}

	childProBlk.header.Timestamp = prntTimestamp
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp equal to parent block timestamp should verify")
	}

	childProBlk.header.Timestamp = time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance).Unix()
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	childProBlk.header.Timestamp = time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance + time.Second).Unix()
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestProposerBlockVerificationPChainHeight(t *testing.T) {
	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM, time.Unix(0, 0)) // enable ProBlks
	proVM.dummyPChainHeight = 666

	prntBlkPChainHeight := proVM.pChainHeight() / 2
	ParentProBlk, _ := NewProBlock(&proVM,
		NewProHeader(ids.ID{}, 0, prntBlkPChainHeight),
		&snowman.TestBlock{}, nil, false) // not signing block, cannot err
	proVM.state.cacheProBlk(&ParentProBlk)

	childHdr := NewProHeader(ParentProBlk.ID(), 0, 0)
	childProBlk, _ := NewProBlock(&proVM, childHdr,
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, false)

	// child P-Chain height must follow parent P-Chain height
	childProBlk.header.PChainHeight = prntBlkPChainHeight - 1
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	childProBlk.header.PChainHeight = prntBlkPChainHeight
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	childProBlk.header.PChainHeight = prntBlkPChainHeight + 1
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be larger than current P-Chain height
	childProBlk.header.PChainHeight = proVM.pChainHeight()
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	childProBlk.header.PChainHeight = proVM.pChainHeight() + 1
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be higher than current P chain height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}
}
