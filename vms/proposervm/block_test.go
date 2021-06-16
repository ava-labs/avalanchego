package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
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
	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	newBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: proVM.now(),
	}
	proHdr := NewProHeader(ids.Empty.Prefix(8), newBlk.Timestamp().Unix(), 100, *pTestCert.Leaf)
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
	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		TimestampV: proVM.now(),
	}
	proHdr := NewProHeader(ids.Empty.Prefix(8), coreBlk.Timestamp().Unix(), 0, *pTestCert.Leaf)
	proBlk, _ := NewProBlock(proVM, proHdr, coreBlk, nil, false) // not signing block, cannot err

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
	_, valVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }
	pCH, err := proVM.pChainHeight()
	if err != nil {
		t.Fatal("could not retrieve P-chain height")
	}
	prntProHdr := NewProHeader(ids.ID{}, 0, pCH, *pTestCert.Leaf)
	prntProBlk, err := NewProBlock(proVM, prntProHdr, &snowman.TestBlock{}, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	childProHdr := NewProHeader(prntProBlk.ID(), 0, pCH, *pTestCert.Leaf)
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
	_, valVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	prntTimestamp := proVM.now().Unix()
	pCH, err := proVM.pChainHeight()
	if err != nil {
		t.Fatal("could not retrieve P-chain height")
	}
	ParentProBlk, err := NewProBlock(proVM,
		NewProHeader(ids.ID{}, prntTimestamp, pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
			pCH, *pTestCert.Leaf),
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
	_, valVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	prntBlkPChainHeight, _ := proVM.pChainHeight()
	prntBlkPChainHeight /= 2
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
	currPChainHeight, _ := proVM.pChainHeight()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(ParentProBlk.ID(), 0, currPChainHeight, *pTestCert.Leaf),
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
		NewProHeader(ParentProBlk.ID(), 0, currPChainHeight+1, *pTestCert.Leaf),
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
