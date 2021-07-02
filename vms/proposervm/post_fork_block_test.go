// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
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
	proBlk := postForkBlock{
		innerBlk: &snowman.TestBlock{},
	}

	// test
	_, err := proBlk.Options()
	if err != snowman.ErrNotOracle {
		t.Fatal("Proposer block should signal that it wraps a block not implementing Options interface with ErrNotOracleBlock error")
	}

	// setup
	proBlk = postForkBlock{
		innerBlk: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options()
	if err != nil {
		t.Fatal("Proposer block should forward wrapped block options if this implements Option interface")
	}
}

// ProposerBlock.Verify tests section
func TestBlockVerify_ParentChecks(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
	pChainHeight := uint64(100)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, fmt.Errorf("unknown block")
		}
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		ParentV:    prntCoreBlk,
		BytesV:     []byte{2},
		TimestampV: prntCoreBlk.Timestamp(),
	}
	childSlb, err := statelessblock.Build(
		ids.Empty, // refer unknown parent
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		Block:    childSlb,
		vm:       proVM,
		innerBlk: childCoreBlk,
		status:   choices.Processing,
	}

	// child block referring unknown parent does not verify
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	} else if err == nil {
		t.Fatal("Block with unknown parent should have different error")
	}

	// child block referring known parent does verify
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(), // refer known parent
		prntProBlk.Timestamp().Add(proposer.MaxDelay),
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("Block with known parent should verify: %s", err)
	}
}

func TestBlockVerify_TimestampChecks(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
	pChainHeight := uint64(100)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, fmt.Errorf("unknown block")
		}
	}

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
		ParentV: prntCoreBlk,
		BytesV:  []byte{2},
	}

	// child block timestamp cannot be lower than parent timestamp
	childCoreBlk.TimestampV = prntTimestamp.Add(-1 * time.Second)
	proVM.Clock.Set(childCoreBlk.TimestampV)
	childSlb, err := statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		Block:    childSlb,
		vm:       proVM,
		innerBlk: childCoreBlk,
		status:   choices.Processing,
	}

	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	} else if err == nil {
		t.Fatal("Old proposer block timestamp should have different error")
	}

	// block cannot arrive before its creator window starts
	blkWinDelay, err := proVM.Delay(childCoreBlk.Height(), pChainHeight, proVM.ctx.NodeID)
	if err != nil {
		t.Fatal("Could not calculate submission window")
	}
	beforeWinStart := prntTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
	proVM.Clock.Set(beforeWinStart)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		beforeWinStart,
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
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
	proVM.Clock.Set(atWindowStart)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		atWindowStart,
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb

	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("Proposer block timestamp at submission window start should verify")
	}

	// block can arrive after its creator window starts
	afterWindowStart := prntTimestamp.Add(blkWinDelay).Add(5 * time.Second)
	proVM.Clock.Set(afterWindowStart)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		afterWindowStart,
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp after submission window start should verify")
	}

	// block can arrive within submission window
	AtSubWindowEnd := proVM.Time().Add(proposer.MaxDelay)
	proVM.Clock.Set(AtSubWindowEnd)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		AtSubWindowEnd,
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	// block timestamp cannot be too much in the future
	afterSubWinEnd := proVM.Time().Add(syncBound).Add(time.Second)
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		afterSubWinEnd,
		pChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err == nil {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestBlockVerify_PChainHeightChecks(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
	pChainHeight := uint64(100)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, fmt.Errorf("unknown block")
		}
	}

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
		ParentV:    prntCoreBlk,
		BytesV:     []byte{2},
		TimestampV: prntProBlk.Timestamp().Add(proposer.MaxDelay),
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		Block:    childSlb,
		vm:       proVM,
		innerBlk: childCoreBlk,
		status:   choices.Processing,
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	} else if err == nil {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb

	proVM.Set(childCoreBlk.Timestamp())
	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one: %s", err)
	}

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = statelessblock.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
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
		proVM.ctx.StakingCert.Leaf,
		childCoreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.Block = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}
}

func TestBlockVerify_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, fmt.Errorf("unknown block")
		}
	}

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build block")
	}

	if err := builtBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	// set error on coreBlock.Verify and recall Verify()
	coreBlk.VerifyV = errors.New("core block verify should only be called once")
	if err := builtBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	// rebuild a block with the same core block
	pChainHeight++
	if _, err := proVM.BuildBlock(); err != nil {
		t.Fatal("could not build block with same core block")
	}
}

// ProposerBlock.Accept tests section
func TestBlockAccept_SetsLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, fmt.Errorf("unknown block")
		}
	}

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

func TestBlockAccept_TwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Time{}) // enable ProBlks
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

// // Pre Fork tests section
// func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, timer.MaxTime) // disable ProBlks

// 	// create parent block ...
// 	prntCoreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{1},
// 		ParentV:    coreGenBlk,
// 		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
// 	coreVM.CantGetBlock = true
// 	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
// 		switch blkID {
// 		case coreGenBlk.ID():
// 			return coreGenBlk, nil
// 		case prntCoreBlk.ID():
// 			return prntCoreBlk, nil
// 		default:
// 			return nil, database.ErrNotFound
// 		}
// 	}
// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.Bytes()):
// 			return coreGenBlk, nil
// 		case bytes.Equal(b, prntCoreBlk.Bytes()):
// 			return prntCoreBlk, nil
// 		default:
// 			return nil, database.ErrNotFound
// 		}
// 	}

// 	prntProBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("Could not build proposer block")
// 	}

// 	// .. create child block ...
// 	childCoreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.GenerateTestID(),
// 			StatusV: choices.Processing,
// 		},
// 		ParentV:    prntCoreBlk,
// 		BytesV:     []byte{2},
// 		TimestampV: prntCoreBlk.Timestamp().Add(proposer.MaxDelay),
// 	}
// 	childProBlk := preForkBlock{
// 		Block: childCoreBlk,
// 		vm:    proVM,
// 	}

// 	// child block referring unknown parent does not verify
// 	err = childProBlk.Verify()
// 	if err == nil {
// 		t.Fatal("Block with unknown parent should not verify")
// 	} else if err == nil {
// 		t.Fatal("Block with unknown parent should have different error")
// 	}

// 	// child block referring known parent does verify
// 	childSlb, err = statelessblock.BuildPreFork(
// 		prntProBlk.ID(), // refer known parent
// 		childCoreBlk.Timestamp(),
// 		proVM.activationTime,
// 		childCoreBlk.Bytes(),
// 		childCoreBlk.ID(),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	childProBlk.Block = childSlb
// 	if err != nil {
// 		t.Fatal("could not sign parent block")
// 	}

// 	if err := childProBlk.Verify(); err != nil {
// 		t.Fatal("Block with known parent should verify")
// 	}
// }

// func TestBlockVerify_PreForkParent(t *testing.T) {
// 	activationTime := time.Now()
// 	_, _, proVM, coreParentBlk := initTestProposerVM(t, activationTime)

// 	if !coreParentBlk.Timestamp().Before(activationTime) {
// 		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
// 	}

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:  []byte{1},
// 		ParentV: coreParentBlk,
// 		VerifyV: nil,
// 	}

// 	// preFork block verifies if parent is before fork activation time
// 	slb, err := statelessblock.BuildPreFork(
// 		proVM.preferred,
// 		coreBlk.Timestamp(),
// 		proVM.activationTime,
// 		coreBlk.Bytes(),
// 		coreBlk.ID(),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk := ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err != nil {
// 		t.Fatal("pre Fork blocks should verify before fork")
// 	}

// 	// postFork block does NOT verify if parent is before fork activation time
// 	slb, err = statelessblock.Build(
// 		proVM.preferred,
// 		coreBlk.Timestamp(),
// 		proVM.activationTime,
// 		100, // pChainHeight,
// 		proVM.ctx.StakingCert.Leaf,
// 		coreBlk.Bytes(),
// 		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk = ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err == nil {
// 		t.Fatal("post Fork blocks should NOT verify before fork activation time")
// 	}
// }

// func TestBlockVerify_AtForkParent(t *testing.T) {
// 	activationTime := time.Now()
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)
// 	proVM.Set(time.Now())

// 	// build parent block at fork activation time ...
// 	coreParentBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{1},
// 		TimestampV: activationTime,
// 		ParentV:    coreGenBlk,
// 		VerifyV:    nil,
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreParentBlk, nil }

// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.Bytes()):
// 			return coreGenBlk, nil
// 		case bytes.Equal(b, coreParentBlk.Bytes()):
// 			return coreParentBlk, nil
// 		default:
// 			t.Fatal("Unknown block")
// 			return nil, nil
// 		}
// 	}
// 	proParentBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("could not build parent block")
// 	}
// 	if !proParentBlk.Timestamp().Equal(activationTime) {
// 		t.Fatal("This test requires parent block 's timestamp to be at fork activation time")
// 	}

// 	// ... preFork block does NOT verify if parent is at fork activation time
// 	coreChildBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(2222),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:  []byte{1},
// 		ParentV: coreParentBlk,
// 		VerifyV: nil,
// 	}
// 	slb, err := statelessblock.BuildPreFork(
// 		proParentBlk.ID(),
// 		coreChildBlk.Timestamp(),
// 		proVM.activationTime,
// 		coreChildBlk.Bytes(),
// 		coreChildBlk.ID(),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk := ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreChildBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err == nil {
// 		t.Fatal("pre Fork blocks should NOT verify if parent is at fork activation time")
// 	}

// 	// postFork block does verify if parent is at fork activation time
// 	coreChildBlk.TimestampV = proParentBlk.Timestamp().Add(proposer.WindowDuration)
// 	slb, err = statelessblock.Build(
// 		proParentBlk.ID(),
// 		coreChildBlk.Timestamp(),
// 		proVM.activationTime,
// 		100, // pChainHeight,
// 		proVM.ctx.StakingCert.Leaf,
// 		coreChildBlk.Bytes(),
// 		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk = ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreChildBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err != nil {
// 		t.Fatal("post Fork blocks should verify at fork activation time")
// 	}
// }

// func TestBlockVerify_PostForkParent(t *testing.T) {
// 	activationTime := time.Now()
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)
// 	proVM.Set(activationTime.Add(time.Second))

// 	// build parent block after fork activation time ...
// 	coreParentBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:     []byte{1},
// 		TimestampV: proVM.Time(),
// 		ParentV:    coreGenBlk,
// 		VerifyV:    nil,
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreParentBlk, nil }

// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.Bytes()):
// 			return coreGenBlk, nil
// 		case bytes.Equal(b, coreParentBlk.Bytes()):
// 			return coreParentBlk, nil
// 		default:
// 			t.Fatal("Unknown block")
// 			return nil, nil
// 		}
// 	}
// 	proParentBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("could not build parent block")
// 	}
// 	if !proParentBlk.Timestamp().After(activationTime) {
// 		t.Fatal("This test requires parent block 's timestamp to be after fork activation time")
// 	}

// 	// ... preFork block does NOT verify if parent is after fork activation time
// 	coreChildBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(2222),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:  []byte{1},
// 		ParentV: coreParentBlk,
// 		VerifyV: nil,
// 	}
// 	slb, err := statelessblock.BuildPreFork(
// 		proParentBlk.ID(),
// 		coreChildBlk.Timestamp(),
// 		proVM.activationTime,
// 		coreChildBlk.Bytes(),
// 		coreChildBlk.ID(),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk := ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreChildBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err == nil {
// 		t.Fatal("pre Fork blocks should NOT verify if parent is after fork activation time")
// 	}

// 	// postFork block does verify if parent is after fork activation time
// 	coreChildBlk.TimestampV = proParentBlk.Timestamp().Add(proposer.WindowDuration)
// 	slb, err = statelessblock.Build(
// 		proParentBlk.ID(),
// 		coreChildBlk.Timestamp(),
// 		proVM.activationTime,
// 		100, // pChainHeight,
// 		proVM.ctx.StakingCert.Leaf,
// 		coreChildBlk.Bytes(),
// 		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
// 	)
// 	if err != nil {
// 		t.Fatal("could not build stateless block")
// 	}
// 	proBlk = ProposerBlock{
// 		Block:   slb,
// 		vm:      proVM,
// 		coreBlk: coreChildBlk,
// 		status:  choices.Processing,
// 	}
// 	if err := proBlk.Verify(); err != nil {
// 		t.Fatal("post Fork blocks should verify after fork activation time")
// 	}
// }

// func TestBlockVerify_PreFork_CoreBlockVerifyIsCalledOnce(t *testing.T) {
// 	// Verify a block once (in this test by building it).
// 	// Show that other verify call would not call coreBlk.Verify()
// 	activationTime := time.Now()
// 	coreVM, _, proVM, coreParentBlk := initTestProposerVM(t, activationTime)

// 	if !coreParentBlk.Timestamp().Before(activationTime) {
// 		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
// 	}

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:  []byte{1},
// 		ParentV: coreParentBlk,
// 		VerifyV: nil,
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

// 	builtBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("could not build block")
// 	}

// 	if err := builtBlk.Verify(); err != nil {
// 		t.Fatal(err)
// 	}

// 	// set error on coreBlock.Verify and recall Verify()
// 	coreBlk.VerifyV = errors.New("core block verify should only be called once")
// 	if err := builtBlk.Verify(); err != nil {
// 		t.Fatal(err)
// 	}
// }

// func TestBlockAccept_PreFork_SetsLastAcceptedBlock(t *testing.T) {
// 	// setup
// 	activationTime := time.Now()
// 	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)

// 	coreBlk := &snowman.TestBlock{
// 		TestDecidable: choices.TestDecidable{
// 			IDV:     ids.Empty.Prefix(1111),
// 			StatusV: choices.Processing,
// 		},
// 		BytesV:  []byte{1},
// 		ParentV: coreGenBlk,
// 	}
// 	coreVM.CantBuildBlock = true
// 	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
// 	coreVM.CantGetBlock = true
// 	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
// 		switch blkID {
// 		case coreGenBlk.ID():
// 			return coreGenBlk, nil
// 		case coreBlk.ID():
// 			return coreBlk, nil
// 		default:
// 			return nil, database.ErrNotFound
// 		}
// 	}
// 	coreVM.CantParseBlock = true
// 	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
// 		switch {
// 		case bytes.Equal(b, coreGenBlk.Bytes()):
// 			return coreGenBlk, nil
// 		case bytes.Equal(b, coreBlk.Bytes()):
// 			return coreBlk, nil
// 		default:
// 			return nil, fmt.Errorf("unknown block")
// 		}
// 	}

// 	builtBlk, err := proVM.BuildBlock()
// 	if err != nil {
// 		t.Fatal("proposerVM could not build block")
// 	}

// 	// test
// 	if err := builtBlk.Accept(); err != nil {
// 		t.Fatal("could not accept block")
// 	}

// 	coreVM.LastAcceptedF = func() (ids.ID, error) {
// 		if coreBlk.Status() == choices.Accepted {
// 			return coreBlk.ID(), nil
// 		}
// 		return coreGenBlk.ID(), nil
// 	}
// 	if acceptedID, err := proVM.LastAccepted(); err != nil {
// 		t.Fatal("could not retrieve last accepted block")
// 	} else if acceptedID != builtBlk.ID() {
// 		t.Fatal("unexpected last accepted ID")
// 	}
// }
