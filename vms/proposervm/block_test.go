package proposervm

import (
	"bytes"
	"crypto"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
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

// TODO: these two below are the first tests I wrote. Remove or move to vm tests
func TestProposerBlockHeaderIsMarshalled(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: proVM.now(),
	}

	slb, err := statelessblock.Build(
		proVM.preferred,
		coreBlk.Timestamp(),
		100, // pChainHeight,
		proVM.stakingCert.Leaf,
		coreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk := ProposerBlock{
		Block:   slb,
		vm:      proVM,
		coreBlk: coreBlk,
		status:  choices.Processing,
	}

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err != nil {
		t.Fatal("failed parsing proposervm.Block. Error:", err)
	}

	if parsedBlk.ID() != proBlk.ID() {
		t.Fatal("Parsed proposerBlock is different than original one")
	}
}

func TestProposerBlockParseFailure(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	coreBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		TimestampV: proVM.now(),
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		return nil, errors.New("Block marshalling failed")
	}
	slb, err := statelessblock.Build(
		proVM.preferred,
		coreBlk.Timestamp(),
		100, // pChainHeight,
		proVM.stakingCert.Leaf,
		coreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk := ProposerBlock{
		Block:   slb,
		vm:      proVM,
		coreBlk: coreBlk,
		status:  choices.Processing,
	}

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err == nil {
		t.Fatal("failed parsing proposervm.Block. Error:", err)
	}

	if parsedBlk != nil {
		t.Fatal("upon failure proposervm.VM.ParseBlock should return nil snowman.Block")
	}
}

// ProposerBlock.Verify tests section
func TestProposerBlockVerificationParent(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		ParentV:    prntCoreBlk,
		BytesV:     []byte{2},
		TimestampV: proVM.now(),
	}
	childSlb, err := statelessblock.Build(
		ids.Empty, // refer unknown parent
		childCoreBlk.Timestamp(),
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := ProposerBlock{
		Block:   childSlb,
		vm:      proVM,
		coreBlk: childCoreBlk,
		status:  choices.Processing,
	}

	// child block referring unknown parent does not verify
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	} else if err != ErrProBlkWrongParent {
		t.Fatal("Block with unknown parent should have different error")
	}

	// child block referring known parent does verify
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(), // refer known parent
		childCoreBlk.Timestamp(),
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Block with known parent should verify")
	}
}

func TestProposerBlockVerificationTimestamp(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(100)

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: proVM.now(),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	prntTimestamp := prntProBlk.Timestamp()

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: prntCoreBlk,
	}

	// child block timestamp cannot be lower than parent timestamp
	childCoreBlk.TimestampV = prntTimestamp.Add(-1 * time.Second)
	childSlb, err := statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := ProposerBlock{
		Block:   childSlb,
		vm:      proVM,
		coreBlk: childCoreBlk,
		status:  choices.Processing,
	}

	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Old proposer block timestamp should have different error")
	}

	// block cannot arrive before its creator window starts
	blkWinDelay, err := proVM.Delay(childCoreBlk.Height(), pChainHeight, proVM.nodeID)
	if err != nil {
		t.Fatal("Could not calculate submission window")
	}
	beforeWinStart := prntTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		beforeWinStart,
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp before submission window should not verify")
	}

	// block can arrive at its creator window starts
	atWindowStart := prntTimestamp.Add(blkWinDelay)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		atWindowStart,
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb

	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp at submission window start should verify")
	}

	// block can arrive after its creator window starts
	afterWindowStart := prntTimestamp.Add(blkWinDelay).Add(5 * time.Second)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		afterWindowStart,
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp after submission window start should verify")
	}

	// block can arrive within submission window
	AtSubWindowEnd := proVM.clock.now().Add(proposer.MaxDelay)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		AtSubWindowEnd,
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	// block timestamp cannot be too much in the future
	afterSubWinEnd := proVM.clock.now().Add(proposer.MaxDelay).Add(time.Second)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		afterSubWinEnd,
		100, // pChainHeight
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err != ErrProBlkBadTimestamp {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestProposerBlockVerificationPChainHeight(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(10)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }
	valVM.CantGetValidatorSet = true
	valVM.GetValidatorsF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[proVM.nodeID] = uint64(10)
		return res, nil
	}

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	prntBlkPChainHeight := pChainHeight
	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: prntCoreBlk,
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := ProposerBlock{
		Block:   childSlb,
		vm:      proVM,
		coreBlk: childCoreBlk,
		status:  choices.Processing,
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be at most equal to current P-Chain height
	currPChainHeight, _ := proVM.PChainHeight()
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight,
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height cannot be larger than current P-Chain height
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight+1,
		proVM.stakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.stakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be higher than current P chain height")
	} else if err != ErrProBlkWrongHeight {
		t.Fatal("Proposer block has wrong height should have different error")
	}
}

func TestVerifyIsCalledOnceOnCoreBlocks(t *testing.T) {
	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

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
		t.Fatal("could not build block")
	}

	// set error on coreBlock.Verify and recall Verify()
	coreBlk.VerifyV = errors.New("core block should be called only once")
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("already verified block would not verify")
	}

	// rebuild a block with the same core block
	pChainHeight++
	if _, err := proVM.BuildBlock(); err != nil {
		t.Fatal("could not build block with same core block")
	}
}

// ProposerBlock.Accept tests section
func TestProposerBlockAcceptSetLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

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
	var pChainHeight uint64
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// generate two blocks with the same core block and store them
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

	pChainHeight = 100 // proBlk1's pChainHeight
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build proBlk1")
	}

	pChainHeight = 200 // proBlk2's pChainHeight
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build proBlk2")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
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
