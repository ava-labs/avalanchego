package proposervm

import (
	"bytes"
	"crypto"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)

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
	childCoreBlk.ParentV = &missing.Block{}
	err = childProBlk.Verify()
	if err == nil {
		t.Fatal("Block with unknown parent should not verify")
	}

	// child block referring known parent does verify
	childCoreBlk.ParentV = prntProBlk
	if err := childProBlk.Verify(); err != nil {
		t.Fatal("Block with known parent should verify")
	}
}

func TestBlockVerify_BlocksBuiltOnPreForkGenesis(t *testing.T) {
	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)
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
		ParentV:    coreGenBlk,
		TimestampV: preActivationTime,
		VerifyV:    nil,
	}
	coreVM.CantBuildBlock = true
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
	postForkStatelessChild, err := statelessblock.Build(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0, // pChainHeight
		proVM.ctx.StakingCert.Leaf,
		coreBlk.Bytes(),
		proVM.ctx.StakingCert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		t.Fatalf("unexpectedly could not build block due to %s", err)
	}
	postForkChild := &postForkBlock{
		Block:    postForkStatelessChild,
		vm:       proVM,
		innerBlk: coreBlk,
		status:   choices.Processing,
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

	coreVM.CantSetPreference = true
	coreVM.SetPreferenceF = func(id ids.ID) error { return nil }
	if err := proVM.SetPreference(preForkChild.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	secondCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2222),
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk,
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.CantBuildBlock = true
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
		ParentV:    secondCoreBlk,
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.CantBuildBlock = true
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
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, activationTime)
	proVM.Set(activationTime)

	// build parent block after fork activation time ...
	coreBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk,
		TimestampV: coreGenBlk.Timestamp(),
		VerifyV:    nil,
	}
	coreVM.CantBuildBlock = true
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
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, timer.MaxTime)

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
