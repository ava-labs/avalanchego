// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

// ProposerBlock Option interface tests section
func TestOracle_PostForkBlock_ImplementsInterface(t *testing.T) {
	// setup
	proBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			innerBlk: &snowman.TestBlock{},
		},
	}

	// test
	_, err := proBlk.Options()
	if err != snowman.ErrNotOracle {
		t.Fatal("Proposer block should signal that it wraps a block not implementing Options interface with ErrNotOracleBlock error")
	}

	// setup
	_, _, proVM, _, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	innerOracleBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV: ids.Empty.Prefix(1111),
			},
			BytesV: []byte{1},
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(2222),
				},
				BytesV: []byte{2},
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(3333),
				},
				BytesV: []byte{3},
			},
		},
	}

	slb, err := block.Build(
		ids.Empty, // refer unknown parent
		time.Time{},
		0, // pChainHeight,
		proVM.ctx.StakingCertLeaf,
		innerOracleBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	proBlk = postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerOracleBlk,
			status:   choices.Processing,
		},
	}

	// test
	_, err = proBlk.Options()
	if err != nil {
		t.Fatal("Proposer block should forward wrapped block options if this implements Option interface")
	}
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkBlock_ParentChecks(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	if err := prntProBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(prntProBlk.ID()); err != nil {
		t.Fatal(err)
	}

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		ParentV:    prntCoreBlk.ID(),
		BytesV:     []byte{2},
		TimestampV: prntCoreBlk.Timestamp(),
	}
	childSlb, err := block.Build(
		ids.Empty, // refer unknown parent
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	// child block referring unknown parent does not verify
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	}

	// child block referring known parent does verify
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(), // refer known parent
		prntProBlk.Timestamp().Add(proposer.MaxDelay),
		pChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err != nil {
		t.Fatal("could not sign parent block")
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("Block with known parent should verify: %s", err)
	}
}

func TestBlockVerify_PostForkBlock_TimestampChecks(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	if err := prntProBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(prntProBlk.ID()); err != nil {
		t.Fatal(err)
	}

	prntTimestamp := prntProBlk.Timestamp()

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV: prntCoreBlk.ID(),
		BytesV:  []byte{2},
	}

	// child block timestamp cannot be lower than parent timestamp
	childCoreBlk.TimestampV = prntTimestamp.Add(-1 * time.Second)
	proVM.Clock.Set(childCoreBlk.TimestampV)
	childSlb, err := block.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Proposer block timestamp too old should not verify")
	}

	// block cannot arrive before its creator window starts
	blkWinDelay, err := proVM.Delay(childCoreBlk.Height(), pChainHeight, proVM.ctx.NodeID)
	if err != nil {
		t.Fatal("Could not calculate submission window")
	}
	beforeWinStart := prntTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
	proVM.Clock.Set(beforeWinStart)
	childSlb, err = block.Build(
		prntProBlk.ID(),
		beforeWinStart,
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp before submission window should not verify")
	}

	// block can arrive at its creator window starts
	atWindowStart := prntTimestamp.Add(blkWinDelay)
	proVM.Clock.Set(atWindowStart)
	childSlb, err = block.Build(
		prntProBlk.ID(),
		atWindowStart,
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb

	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("Proposer block timestamp at submission window start should verify")
	}

	// block can arrive after its creator window starts
	afterWindowStart := prntTimestamp.Add(blkWinDelay).Add(5 * time.Second)
	proVM.Clock.Set(afterWindowStart)
	childSlb, err = block.Build(
		prntProBlk.ID(),
		afterWindowStart,
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp after submission window start should verify")
	}

	// block can arrive within submission window
	AtSubWindowEnd := proVM.Time().Add(proposer.MaxDelay)
	proVM.Clock.Set(AtSubWindowEnd)
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		AtSubWindowEnd,
		pChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Proposer block timestamp within submission window should verify")
	}

	// block timestamp cannot be too much in the future
	afterSubWinEnd := proVM.Time().Add(maxSkew).Add(time.Second)
	childSlb, err = block.Build(
		prntProBlk.ID(),
		afterSubWinEnd,
		pChainHeight,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err == nil {
		t.Fatal("Proposer block timestamp after submission window should not verify")
	} else if err == nil {
		t.Fatal("Proposer block timestamp after submission window should have different error")
	}
}

func TestBlockVerify_PostForkBlock_PChainHeightChecks(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return prntCoreBlk, nil }
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	if err := prntProBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(prntProBlk.ID()); err != nil {
		t.Fatal(err)
	}

	prntBlkPChainHeight := pChainHeight

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV:    prntCoreBlk.ID(),
		BytesV:     []byte{2},
		TimestampV: prntProBlk.Timestamp().Add(proposer.MaxDelay),
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := block.Build(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	} else if err == nil {
		t.Fatal("Proposer block has wrong height should have different error")
	}

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb

	proVM.Set(childCoreBlk.Timestamp())
	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one: %s", err)
	}

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height can be equal to current P-Chain height
	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight()
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be equal to current p chain height")
	}

	// block P-Chain height cannot be at higher than current P-Chain height
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight*2,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != errPChainHeightNotReached {
		t.Fatal("ProBlock's P-Chain-Height cannot be larger than current p chain height")
	}
}

func TestBlockVerify_PostForkBlockBuiltOnOption_PChainHeightChecks(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }
	// proVM.SetStartTime(timer.MaxTime) // switch off scheduler for current test

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
		},
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{2},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: oracleCoreBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: oracleCoreBlk.Timestamp(),
		},
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	oracleBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	if err := oracleBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(oracleBlk.ID()); err != nil {
		t.Fatal(err)
	}

	// retrieve one option and verify block built on it
	postForkOracleBlk, ok := oracleBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}
	parentBlk := opts[0]

	if err := parentBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(parentBlk.ID()); err != nil {
		t.Fatal(err)
	}

	prntBlkPChainHeight := pChainHeight

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV:    oracleCoreBlk.opts[0].ID(),
		BytesV:     []byte{2},
		TimestampV: parentBlk.Timestamp().Add(proposer.MaxDelay),
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := block.Build(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.ctx.StakingCertLeaf,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	if err := childProBlk.Verify(); err == nil {
		t.Fatal("ProBlock's P-Chain-Height cannot be lower than parent ProBlock's one")
	}

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb

	proVM.Set(childCoreBlk.Timestamp())
	if err := childProBlk.Verify(); err != nil {
		t.Fatalf("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one: %s", err)
	}

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be larger or equal than parent ProBlock's one")
	}

	// block P-Chain height can be equal to current P-Chain height
	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight()
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("ProBlock's P-Chain-Height can be equal to current p chain height")
	}

	// block P-Chain height cannot be at higher than current P-Chain height
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight*2,
		childCoreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}
	childProBlk.SignedBlock = childSlb
	if err := childProBlk.Verify(); err != errPChainHeightNotReached {
		t.Fatal("ProBlock's P-Chain-Height cannot be larger than current p chain height")
	}
}

func TestBlockVerify_PostForkBlock_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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
func TestBlockAccept_PostForkBlock_SetsLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
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

func TestBlockAccept_PostForkBlock_TwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	var pChainHeight uint64
	valState.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }

	// generate two blocks with the same core block and store them
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	pChainHeight = optimalHeightDelay // proBlk1's pChainHeight
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build proBlk1")
	}

	pChainHeight = optimalHeightDelay + 1 // proBlk2's pChainHeight
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

// ProposerBlock.Reject tests section
func TestBlockReject_PostForkBlock_InnerBlockIsNotRejected(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	sb, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build block")
	}
	proBlk, ok := sb.(*postForkBlock)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if err := proBlk.Reject(); err != nil {
		t.Fatal("could not reject block")
	}

	if proBlk.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proBlk.innerBlk.Status() == choices.Rejected {
		t.Fatal("block rejection unduly changed inner block status")
	}
}

func TestBlockVerify_PostForkBlock_ShouldBePostForkOption(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
	}
	coreOpt0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    oracleCoreBlk.ID(),
		TimestampV: oracleCoreBlk.Timestamp(),
	}
	coreOpt1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    oracleCoreBlk.ID(),
		TimestampV: oracleCoreBlk.Timestamp(),
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		coreOpt0,
		coreOpt1,
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	if err := parentBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(parentBlk.ID()); err != nil {
		t.Fatal(err)
	}

	// retrieve options ...
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}
	if _, ok := opts[0].(*postForkOption); !ok {
		t.Fatal("unexpected option type")
	}

	// ... and verify them the first time
	if err := opts[0].Verify(); err != nil {
		t.Fatal("option 0 should verify")
	}
	if err := opts[1].Verify(); err != nil {
		t.Fatal("option 1 should verify")
	}

	// Build the child
	statelessChild, err := block.Build(
		postForkOracleBlk.ID(),
		postForkOracleBlk.Timestamp().Add(proposer.WindowDuration),
		postForkOracleBlk.PChainHeight(),
		proVM.ctx.StakingCertLeaf,
		oracleCoreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("failed to build new child block")
	}

	invalidChild, err := proVM.ParseBlock(statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify()
	if err == nil {
		t.Fatal("Should have failed to verify a child that was signed when it should be an oracle block")
	}
}

func TestBlockVerify_PostForkBlock_PChainTooLow(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 5)
	proVM.Set(coreGenBlk.Timestamp())

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
	}

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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	statelessChild, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		4,
		coreBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("failed to build new child block")
	}

	invalidChild, err := proVM.ParseBlock(statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify()
	if err == nil {
		t.Fatal("Should have failed to verify a child that was signed when it should be an oracle block")
	}
}
