package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/staking"
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
	coreVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	proHdr := NewProHeader(ids.Empty.Prefix(8), proVM.now().Unix(), 100, *pTestCert.Leaf)
	newBlk := &snowman.TestBlock{
		BytesV: []byte{1},
	}
	proBlk, _ := NewProBlock(proVM, proHdr, newBlk, nil, false) // not signing block, cannot err

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
	coreVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	proHdr := NewProHeader(ids.Empty.Prefix(8), proVM.now().Unix(), 0, *pTestCert.Leaf)
	proBlk, _ := NewProBlock(proVM, proHdr, &snowman.TestBlock{}, nil, false) // not signing block, cannot err

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
	_, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	prntProHdr := NewProHeader(ids.ID{}, 0, proVM.pChainHeight(), *pTestCert.Leaf)
	prntProBlk, err := NewProBlock(proVM, prntProHdr, &snowman.TestBlock{}, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	childProHdr := NewProHeader(prntProBlk.ID(), 0, proVM.pChainHeight(), *pTestCert.Leaf)
	childProBlk, err := NewProBlock(proVM, childProHdr, &snowman.TestBlock{
		VerifyV: nil,
	}, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	// Parent block not store yet
	err = childProBlk.Verify()
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
	_, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	prntTimestamp := proVM.now().Unix()
	ParentProBlk, err := NewProBlock(proVM,
		NewProHeader(ids.ID{}, prntTimestamp, proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{}, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}
	proVM.state.cacheProBlk(&ParentProBlk)

	// child block timestamp cannot be lower than parent timestamp
	timeBeforeParent := time.Unix(prntTimestamp, 0).Add(-1 * time.Second).Unix()
	childProBlk, err := NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			timeBeforeParent,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Old proposer block timestamp should have different error")
	}

	// block can arrive at its creator window starts
	firstWindowsStart := prntTimestamp
	proVM.mockedValPos = 0 // creator windows is the first
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			firstWindowsStart,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp equal to parent block timestamp should verify")
	}

	// block cannot arrive before its creator window starts
	proVM.mockedValPos = 1 // creator windows is the second
	blkNodeID := ids.ShortID{}
	winDelay := proVM.BlkSubmissionDelay(childProBlk.header.pChainHeight, blkNodeID)
	beforeWindowStart := time.Unix(prntTimestamp, 0).Add(winDelay - time.Second).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			beforeWindowStart,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp before submission window should not verify")
	}

	// block can arrive at its creator window starts
	atWindowStart := time.Unix(prntTimestamp, 0).Add(winDelay).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			atWindowStart,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp at submission window start should verify")
	}

	// block can arrive after its creator window starts
	afterWindowStart := time.Unix(prntTimestamp, 0).Add(winDelay + 5*time.Second).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			afterWindowStart,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp after submission window start should verify")
	}

	// block can arrive within submission window
	AtSubWindowEnd := time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			AtSubWindowEnd,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	// block timestamp cannot be too much in the future
	afterSubWinEnd := time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance + time.Second).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(),
			afterSubWinEnd,
			proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestProposerBlockVerificationPChainHeight(t *testing.T) {
	_, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	proVM.dummyPChainHeight = 666

	prntBlkPChainHeight := proVM.pChainHeight() / 2
	ParentProBlk, err := NewProBlock(proVM,
		NewProHeader(ids.ID{}, 0, prntBlkPChainHeight, *pTestCert.Leaf),
		&snowman.TestBlock{}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	proVM.state.cacheProBlk(&ParentProBlk)

	// child P-Chain height must not precede parent P-Chain height
	childProBlk, err := NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, prntBlkPChainHeight-1, *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	// child P-Chain height can be equal to parent P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, prntBlkPChainHeight, *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// child P-Chain height may follow parent P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, prntBlkPChainHeight+1, *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be at most equal to current P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, proVM.pChainHeight(), *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be larger than current P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, proVM.pChainHeight()+1, *pTestCert.Leaf),
		&snowman.TestBlock{
			VerifyV: nil,
		}, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be higher than current P chain height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}
}

func TestSIMPLE_SIGNATURE_TEST(t *testing.T) {
	cert, err := staking.NewTLSCert()
	if err != nil {
		t.Fatal("Could not generate dummy StakerCert")
	}

	coreVM := &block.TestVM{}
	proVM := NewProVM(coreVM, time.Unix(0, 0))
	proVM.stakingCert = *cert
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		BytesV: []byte{0},
	}

	proHdr := NewProHeader(ids.ID{}, 0, 0, *cert.Leaf)
	/*proBlk*/ _, err = NewProBlock(&proVM, proHdr, coreBlk, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}
}
