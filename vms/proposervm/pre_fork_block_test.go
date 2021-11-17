// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

func TestOracle_PreForkBlkImplementsInterface(t *testing.T) {
	// setup
	proBlk := preForkBlock{
		Block: &snowman.TestBlock{},
	}

	// test
	_, err := proBlk.Options()
	if err != snowman.ErrNotOracle {
		t.Fatal("Proposer block should signal that it wraps a block not implementing Options interface with ErrNotOracleBlock error")
	}

	// setup
	proBlk = preForkBlock{
		Block: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options()
	if err != nil {
		t.Fatal("Proposer block should forward wrapped block options if this implements Option interface")
	}
}

func TestOracle_PreForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0)

	// create pre fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{1},
			ParentV: coreGenBlk.ID(),
		},
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{2},
			ParentV: oracleCoreBlk.ID(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{3},
			ParentV: oracleCoreBlk.ID(),
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

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build pre fork oracle block")
	}

	// retrieve options ...
	preForkOracleBlk, ok := parentBlk.(*preForkBlock)
	if !ok {
		t.Fatal("expected pre fork block")
	}
	opts, err := preForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from pre fork oracle block")
	}
	if err := opts[0].Verify(); err != nil {
		t.Fatal("option should verify")
	}

	// ... show a block can be built on top of an option
	if err := proVM.SetPreference(opts[0].ID()); err != nil {
		t.Fatal("could not set preference")
	}

	lastCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(4444),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{4},
			ParentV: oracleCoreBlk.opts[0].ID(),
		},
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return lastCoreBlk, nil }

	preForkChild, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build pre fork block on pre fork option block")
	}
	if _, ok := preForkChild.(*preForkBlock); !ok {
		t.Fatal("expected pre fork block built on pre fork option block")
	}
}

func TestOracle_PostForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)

	// create pre fork oracle block pre activation time...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: activationTime.Add(-1 * time.Second),
		},
	}

	// ... whose options are post activation time
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{2},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: activationTime.Add(time.Second),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: activationTime.Add(time.Second),
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

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build pre fork oracle block")
	}

	// retrieve options ...
	preForkOracleBlk, ok := parentBlk.(*preForkBlock)
	if !ok {
		t.Fatal("expected pre fork block")
	}
	opts, err := preForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from pre fork oracle block")
	}
	if err := opts[0].Verify(); err != nil {
		t.Fatal("option should verify")
	}

	// ... show a block can be built on top of an option
	if err := proVM.SetPreference(opts[0].ID()); err != nil {
		t.Fatal("could not set preference")
	}

	lastCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(4444),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{4},
			ParentV: oracleCoreBlk.opts[0].ID(),
		},
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return lastCoreBlk, nil }

	postForkChild, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build pre fork block on pre fork option block")
	}
	if _, ok := postForkChild.(*postForkBlock); !ok {
		t.Fatal("expected pre fork block built on pre fork option block")
	}
}

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)

	if !coreGenBlk.Timestamp().Before(activationTime) {
		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
	}

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
			return nil, database.ErrNotFound
		}
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	prntProBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		TimestampV: prntCoreBlk.Timestamp().Add(proposer.MaxDelay),
	}
	childProBlk := preForkBlock{
		Block: childCoreBlk,
		vm:    proVM,
	}

	// child block referring unknown parent does not verify
	childCoreBlk.ParentV = ids.Empty
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	}

	// child block referring known parent does verify
	childCoreBlk.ParentV = prntProBlk.ID()
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Block with known parent should verify")
	}
}

func TestBlockVerify_BlocksBuiltOnPreForkGenesis(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	if !coreGenBlk.Timestamp().Before(activationTime) {
		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
	}
	preActivationTime := activationTime.Add(-1 * time.Second)
	proVM.Set(preActivationTime)

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: preActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// preFork block verifies if parent is before fork activation time
	preForkChild, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	} else if _, ok := preForkChild.(*preForkBlock); !ok {
		t.Fatal("expected preForkBlock")
	}

	if err := preForkChild.Verify(); err != nil {
		t.Fatal("pre Fork blocks should verify before fork")
	}

	// postFork block does NOT verify if parent is before fork activation time
	postForkStatelessChild, err := block.Build(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0, // pChainHeight
		proVM.ctx.StakingCertLeaf,
		coreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	}
	postForkChild := &postForkBlock{
		SignedBlock: postForkStatelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: coreBlk,
			status:   choices.Processing,
		},
	}

	if !postForkChild.Timestamp().Before(activationTime) {
		t.Fatal("This test requires postForkChild to be before fork activation time")
	}
	if err := postForkChild.Verify(); err == nil {
		t.Fatal("post Fork blocks should NOT verify before fork")
	}

	// once activation time is crossed postForkBlock are produced
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreVM.SetPreferenceF = func(id ids.ID) error { return nil }
	if err := proVM.SetPreference(preForkChild.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	secondCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2222),
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk.ID(),
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return secondCoreBlk, nil }
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			t.Fatal("attempt to get unknown block")
			return nil, nil
		}
	}

	lastPreForkBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	} else if _, ok := lastPreForkBlk.(*preForkBlock); !ok {
		t.Fatal("expected preForkBlock")
	}

	if err := lastPreForkBlk.Verify(); err != nil {
		t.Fatal("pre Fork blocks should verify before fork")
	}

	if err := proVM.SetPreference(lastPreForkBlk.ID()); err != nil {
		t.Fatal("could not set preference")
	}
	thirdCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(333),
		},
		BytesV:     []byte{3},
		ParentV:    secondCoreBlk.ID(),
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return thirdCoreBlk, nil }
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		switch id {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case secondCoreBlk.ID():
			return secondCoreBlk, nil
		default:
			t.Fatal("attempt to get unknown block")
			return nil, nil
		}
	}

	firstPostForkBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	} else if _, ok := firstPostForkBlk.(*postForkBlock); !ok {
		t.Fatal("expected preForkBlock")
	}

	if err := firstPostForkBlk.Verify(); err != nil {
		t.Fatal("pre Fork blocks should verify before fork")
	}
}

func TestBlockVerify_BlocksBuiltOnPostForkGenesis(t *testing.T) {
	activationTime := genesisTimestamp.Add(-1 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	proVM.Set(activationTime)

	// build parent block after fork activation time ...
	coreBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlock, nil }

	// postFork block verifies if parent is after fork activation time
	postForkChild, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	} else if _, ok := postForkChild.(*postForkBlock); !ok {
		t.Fatal("expected postForkBlock")
	}

	if err := postForkChild.Verify(); err != nil {
		t.Fatal("post Fork blocks should verify after fork")
	}

	// preFork block does NOT verify if parent is after fork activation time
	preForkChild := preForkBlock{
		Block: coreBlock,
		vm:    proVM,
	}
	if err := preForkChild.Verify(); err == nil {
		t.Fatal("pre Fork blocks should NOT verify after fork")
	}
}

func TestBlockAccept_PreFork_SetsLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0)

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
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

// ProposerBlock.Reject tests section
func TestBlockReject_PreForkBlock_InnerBlockIsRejected(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	sb, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build block")
	}
	proBlk, ok := sb.(*preForkBlock)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if err := proBlk.Reject(); err != nil {
		t.Fatal("could not reject block")
	}

	if proBlk.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proBlk.Block.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}
}

func TestBlockVerify_ForkBlockIsOracleBlock(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	if !coreGenBlk.Timestamp().Before(activationTime) {
		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
	}
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreBlkID := ids.GenerateTestID()
	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: postActivationTime,
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
		},
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(coreBlk.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := firstBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	oracleBlock, ok := firstBlock.(snowman.OracleBlock)
	if !ok {
		t.Fatal("should have returned an oracle block")
	}

	options, err := oracleBlock.Options()
	if err != nil {
		t.Fatal(err)
	}

	if err := options[0].Verify(); err != nil {
		t.Fatal(err)
	}

	if err := options[1].Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestBlockVerify_ForkBlockIsOracleBlockButChildrenAreSigned(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	if !coreGenBlk.Timestamp().Before(activationTime) {
		t.Fatal("This test requires parent block 's timestamp to be before fork activation time")
	}
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreBlkID := ids.GenerateTestID()
	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: postActivationTime,
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
		},
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(coreBlk.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := firstBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	slb, err := block.Build(
		firstBlock.ID(), // refer unknown parent
		firstBlock.Timestamp(),
		0, // pChainHeight,
		proVM.ctx.StakingCertLeaf,
		coreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.ctx.StakingLeafSigner,
	)
	if err != nil {
		t.Fatal("could not build stateless block")
	}

	invalidChild, err := proVM.ParseBlock(slb.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify()
	if err == nil {
		t.Fatal("Should have failed to verify a child that was signed when it should be a pre fork block")
	}
}
