package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

type TestOptionsBlock struct {
	snowman.TestBlock
}

func (tob TestOptionsBlock) Options() ([2]snowman.Block, error) {
	return [2]snowman.Block{}, nil
}

// ProposerBlock Option interface tests section
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

// TODO: these two below are the first tests I wrote. Remove or move to vm tests
func TestProposerBlockHeaderIsMarshalled(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	newBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: proVM.now(),
	}
	proHdr := NewProHeader(ids.Empty.Prefix(8), newBlk.Timestamp().Unix(), 100, *pTestCert.Leaf)
	proBlk, _ := NewProBlock(proVM, proHdr, newBlk, choices.Processing, nil, false) // not signing block, cannot err

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
	proBlk, _ := NewProBlock(proVM, proHdr, coreBlk, choices.Processing, nil, false) // not signing block, cannot err

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

// ProposerBlock.Verify tests section
func TestProposerBlockVerificationParent(t *testing.T) {
	_, valVM, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }
	pCH, err := proVM.pChainHeight()
	if err != nil {
		t.Fatal("could not retrieve P-chain height")
	}

	prntCoreBlk := snowman.TestBlock{}
	prntProHdr := NewProHeader(ids.Empty, 0, pCH, *pTestCert.Leaf)
	prntProBlk, err := NewProBlock(proVM,
		prntProHdr, &prntCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	childCoreBlk := snowman.TestBlock{
		ParentV: &prntCoreBlk,
	}
	childProHdr := NewProHeader(prntProBlk.ID(), 0, pCH, *pTestCert.Leaf)
	childProBlk, err := NewProBlock(proVM, childProHdr, &childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	// Parent block not store yet
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	} else if err != ErrProBlkWrongParent {
		t.Fatal("Block with unknown parent should have different error")
	}

	// now store parentBlock
	if err := proVM.state.storeProBlk(&prntProBlk); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

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

	prntCoreBlk := snowman.TestBlock{}
	parentProBlk, err := NewProBlock(proVM,
		NewProHeader(ids.Empty, prntTimestamp, pCH, *pTestCert.Leaf),
		&prntCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign parent block")
	}
	if err := proVM.state.storeProBlk(&parentProBlk); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	// child block timestamp cannot be lower than parent timestamp
	timeBeforeParent := time.Unix(prntTimestamp, 0).Add(-1 * time.Second).Unix()
	childCoreBlk := snowman.TestBlock{
		ParentV: &prntCoreBlk,
	}
	childProBlk, err := NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(),
			timeBeforeParent,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
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
		NewProHeader(parentProBlk.ID(),
			firstWindowsStart,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
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
		NewProHeader(parentProBlk.ID(),
			beforeWindowStart,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp before submission window should not verify")
	}

	// block can arrive at its creator window starts
	atWindowStart := time.Unix(prntTimestamp, 0).Add(winDelay).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(),
			atWindowStart,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp at submission window start should verify")
	}

	// block can arrive after its creator window starts
	afterWindowStart := time.Unix(prntTimestamp, 0).Add(winDelay + 5*time.Second).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(),
			afterWindowStart,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp after submission window start should verify")
	}

	// block can arrive within submission window
	AtSubWindowEnd := time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(),
			AtSubWindowEnd,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	// block timestamp cannot be too much in the future
	afterSubWinEnd := time.Unix(prntTimestamp, 0).Add(BlkSubmissionTolerance + time.Second).Unix()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(),
			afterSubWinEnd,
			pCH, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
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
	prntCoreBlk := snowman.TestBlock{}
	parentProBlk, err := NewProBlock(proVM,
		NewProHeader(ids.Empty, 0, prntBlkPChainHeight, *pTestCert.Leaf),
		&prntCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := proVM.state.storeProBlk(&parentProBlk); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	// child P-Chain height must not precede parent P-Chain height
	childCoreBlk := snowman.TestBlock{
		ParentV: &prntCoreBlk,
	}
	childProBlk, err := NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(), 0, prntBlkPChainHeight-1, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
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
		NewProHeader(parentProBlk.ID(), 0, prntBlkPChainHeight, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// child P-Chain height may follow parent P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(), 0, prntBlkPChainHeight+1, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be at most equal to current P-Chain height
	currPChainHeight, _ := proVM.pChainHeight()
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(), 0, currPChainHeight, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be larger than current P-Chain height
	childProBlk, err = NewProBlock(proVM,
		NewProHeader(parentProBlk.ID(), 0, currPChainHeight+1, *pTestCert.Leaf),
		&childCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign child block")
	}
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be higher than current P chain height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}
}

// ProposerBlock.Accept tests section
func TestProposerBlockAcceptSetLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

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

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	// test
	if err := builtBlk.Accept(); err != nil {
		t.Fatal("could not accept block")
	}

	coreVM.LastAcceptedF = func() (ids.ID, error) {
		if coreBlk.Status() == choices.Accepted {
			return coreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	if acceptedID, err := proVM.LastAccepted(); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != builtBlk.ID() {
		t.Fatal("unexpected last accepted ID")
	}
}

func TestTwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate two blocks with the same core block and store them
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) {
		if coreBlk.Status() == choices.Accepted {
			return coreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr1 := NewProHeader(proGenBlkID, coreBlk.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk1, err := NewProBlock(proVM, proHdr1, coreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if err := proVM.state.storeProBlk(&proBlk1); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	proHdr2 := NewProHeader(proGenBlkID, coreBlk.Timestamp().Add(time.Second).Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk2, err := NewProBlock(proVM, proHdr2, coreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if err := proVM.state.storeProBlk(&proBlk2); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("Test requires proBlk1 and proBlk2 to be different")
	}

	// set proBlk1 as preferred
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("could not accept proBlk1")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("coreBlk should have been accepted")
	}

	if acceptedID, err := proVM.LastAccepted(); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != proBlk1.ID() {
		t.Fatal("unexpected last accepted ID")
	}
}

func TestAccept_SingleBlock(t *testing.T) {
	// there is just one ProBlock with no siblings being accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate one proposer block...
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
	proBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk.Status() != choices.Processing {
		t.Fatal("Newly built pro blocks must have processing status")
	}

	// ..accept it and check that both it and its core block are accepted
	if err := proBlk.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk.Status() != choices.Accepted {
		t.Fatal("accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("accepted core block has wrong status")
	}
}

func TestAccept_BlockConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings;
	// Once proBlk1 is accepted, proBlk2 is rejected
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2 with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	// ..accept proBlk2 and check that proBlk2 is accepted and proBlk1 rejected
	if err := proBlk2.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk1.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk1.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}
}

func TestAccept_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// Once proBlk1 is accepted, proBlk2 and proBlk3 are rejected
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..accept proBlk1 and check that proBlk1 is accepted and proBlk2/proBlk3 are  rejected
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}

	if proBlk3.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}
}

func TestAccept_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// Once proBlk1 is accepted, proBlk2 and proBlk3 are rejected, while coreBlk is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	currentPChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..accept proBlk1 and check that proBlk1 is accepted and proBlk2/proBlk3 are  rejected
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}

	if proBlk3.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Child core block has wrong status")
	}
}

// TODO: accept out-of-order test, just to document behavior. IF ACCEPT CAN COME CHILDREN FIRST

// ProposerBlock.Reject tests section
func TestReject_BlockConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings;
	// ProBlk2 is rejected but its core not; ProBlk1 and core blocks statues as set on proBlk1's Accept
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2 with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	// ..reject proBlk2 and check that other blocks statuses are unaffected
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject proposer block")
	}

	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejecting Pro block should not affect core block")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("Rejection of a sibling should not affect block status")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("Rejection of a sibling should not affect core block status")
	}

	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block should stay rejected upon sibling's accept")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("this core block should be rejected one sibling is accepted")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("problk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreblk1 should be accepted")
	}
}

func TestReject_ParentFirst_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// ProBlk2 and ProBlk3 are rejected in this order before ProBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..Reject ProBlk2 ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Processing { // TODO: propagate rejection immediately
		t.Fatal("Rejection of proBlock ancestor DOES NOT cause YET rejection of its descendants")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should not be rejected")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	//  ... and ProBlk3,...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should be rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Acceptance of sibling should cause rejection of coreBlk2")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Acceptance of uncle should cause rejection of coreBlk2")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
}

func TestReject_ChildFirst_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// ProBlk3 and ProBlk2 are rejected in this order before ProBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	//  ... Reject ProBlk3 ...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should be rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("ProBlk2 should still be processing")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("coreBlk2 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of siblings' descendants")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of siblings' descendants")
	}

	// ... and ProBlk2, ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("proBlk2 should stay Rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Acceptance of sibling should cause rejection of coreBlk2")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Acceptance of uncle should cause rejection of coreBlk3")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
}

func TestReject_ParentFirst_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// proBlk2 and proBlk3 are rejected in this order, then proBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	currentPChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..Reject ProBlk2 ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Processing { // TODO: propagate rejection immediately
		t.Fatal("Rejection of proBlock ancestor DOES NOT cause YET rejection of its descendants")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	//  ... and ProBlk3,...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
}

func TestReject_ChildFirst_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// proBlk3 and proBlk2 are rejected in this order, then proBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	currentPChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..Reject ProBlk3 ...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("ProBlk2 should be processing")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of siblings' descendants")
	}

	//  ... and ProBlk2,...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("proBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
}
