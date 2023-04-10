// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var _ snowman.OracleBlock = (*TestOptionsBlock)(nil)

type TestOptionsBlock struct {
	snowman.TestBlock
	opts    [2]snowman.Block
	optsErr error
}

func (tob TestOptionsBlock) Options(context.Context) ([2]snowman.Block, error) {
	return tob.opts, tob.optsErr
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkOption_ParentChecks(t *testing.T) {
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

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	if err := parentBlk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(context.Background(), parentBlk.ID()); err != nil {
		t.Fatal(err)
	}

	// retrieve options ...
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options(context.Background())
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}
	if _, ok := opts[0].(*postForkOption); !ok {
		t.Fatal("unexpected option type")
	}

	// ... and verify them
	if err := opts[0].Verify(context.Background()); err != nil {
		t.Fatal("option 0 should verify")
	}
	if err := opts[1].Verify(context.Background()); err != nil {
		t.Fatal("option 1 should verify")
	}

	// show we can build on options
	if err := proVM.SetPreference(context.Background(), opts[0].ID()); err != nil {
		t.Fatal("could not set preference")
	}

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(4444),
			StatusV: choices.Processing,
		},
		ParentV:    oracleCoreBlk.opts[0].ID(),
		BytesV:     []byte{4},
		TimestampV: oracleCoreBlk.opts[0].Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return childCoreBlk, nil
	}
	proVM.Set(childCoreBlk.Timestamp())

	proChild, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build on top of option")
	}
	if _, ok := proChild.(*postForkBlock); !ok {
		t.Fatal("unexpected block type")
	}
	if err := proChild.Verify(context.Background()); err != nil {
		t.Fatal("block built on option does not verify")
	}
}

// ProposerBlock.Accept tests section
func TestBlockVerify_PostForkOption_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
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

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	if err := parentBlk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := proVM.SetPreference(context.Background(), parentBlk.ID()); err != nil {
		t.Fatal(err)
	}

	// retrieve options ...
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options(context.Background())
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}
	if _, ok := opts[0].(*postForkOption); !ok {
		t.Fatal("unexpected option type")
	}

	// ... and verify them the first time
	if err := opts[0].Verify(context.Background()); err != nil {
		t.Fatal("option 0 should verify")
	}
	if err := opts[1].Verify(context.Background()); err != nil {
		t.Fatal("option 1 should verify")
	}

	// set error on coreBlock.Verify and recall Verify()
	coreOpt0.VerifyV = errDuplicateVerify
	coreOpt1.VerifyV = errDuplicateVerify

	// ... and verify them again. They verify without call to innerBlk
	if err := opts[0].Verify(context.Background()); err != nil {
		t.Fatal("option 0 should verify")
	}
	if err := opts[1].Verify(context.Background()); err != nil {
		t.Fatal("option 1 should verify")
	}
}

func TestBlockAccept_PostForkOption_SetsLastAcceptedBlock(t *testing.T) {
	// setup
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

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	// accept oracle block
	if err := parentBlk.Accept(context.Background()); err != nil {
		t.Fatal("could not accept block")
	}

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if oracleCoreBlk.Status() == choices.Accepted {
			return oracleCoreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	if acceptedID, err := proVM.LastAccepted(context.Background()); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != parentBlk.ID() {
		t.Fatal("unexpected last accepted ID")
	}

	// accept one of the options
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options(context.Background())
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := opts[0].Accept(context.Background()); err != nil {
		t.Fatal("could not accept option")
	}

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if oracleCoreBlk.opts[0].Status() == choices.Accepted {
			return oracleCoreBlk.opts[0].ID(), nil
		}
		return oracleCoreBlk.ID(), nil
	}
	if acceptedID, err := proVM.LastAccepted(context.Background()); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != opts[0].ID() {
		t.Fatal("unexpected last accepted ID")
	}
}

// ProposerBlock.Reject tests section
func TestBlockReject_InnerBlockIsNotRejected(t *testing.T) {
	// setup
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

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
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

	builtBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	// reject oracle block
	if err := builtBlk.Reject(context.Background()); err != nil {
		t.Fatal("could not reject block")
	}
	proBlk, ok := builtBlk.(*postForkBlock)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if proBlk.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proBlk.innerBlk.Status() == choices.Rejected {
		t.Fatal("block rejection unduly changed inner block status")
	}

	// reject an option
	postForkOracleBlk, ok := builtBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options(context.Background())
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := opts[0].Reject(context.Background()); err != nil {
		t.Fatal("could not accept option")
	}
	proOpt, ok := opts[0].(*postForkOption)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if proOpt.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proOpt.innerBlk.Status() == choices.Rejected {
		t.Fatal("block rejection unduly changed inner block status")
	}
}

func TestBlockVerify_PostForkOption_ParentIsNotOracleWithError(t *testing.T) {
	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		optsErr: snowman.ErrNotOracle,
	}

	coreChildBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk.ID(),
		HeightV:    coreBlk.Height() + 1,
		TimestampV: coreBlk.Timestamp(),
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreChildBlk.ID():
			return coreChildBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreChildBlk.Bytes()):
			return coreChildBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	postForkBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	_, err = postForkBlk.Options(context.Background())
	if err != snowman.ErrNotOracle {
		t.Fatal("should have reported that the block isn't an oracle block")
	}

	// Build the child
	statelessChild, err := block.BuildOption(
		postForkBlk.ID(),
		coreChildBlk.Bytes(),
	)
	if err != nil {
		t.Fatal("failed to build new child block")
	}

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	if err == nil {
		t.Fatal("Should have failed to verify a child that should have been signed")
	}
}

func TestOptionTimestampValidity(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, db := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	coreOracleBlkID := ids.GenerateTestID()
	coreOracleBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreOracleBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			HeightV:    coreGenBlk.Height() + 1,
			TimestampV: coreGenBlk.Timestamp().Add(time.Second),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreOracleBlkID,
				TimestampV: coreGenBlk.Timestamp().Add(time.Second),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreOracleBlkID,
				TimestampV: coreGenBlk.Timestamp().Add(time.Second),
			},
		},
	}
	statelessBlock, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		0,
		coreOracleBlk.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	statefulBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	statefulOracleBlock, ok := statefulBlock.(snowman.OracleBlock)
	if !ok {
		t.Fatal("should have reported as an oracle block")
	}

	options, err := statefulOracleBlock.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	option := options[0]
	if err := option.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := statefulBlock.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		t.Fatal("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		t.Fatal("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	expectedTime := coreGenBlk.Timestamp()
	if optionTime := option.Timestamp(); !optionTime.Equal(expectedTime) {
		t.Fatalf("wrong time returned expected %s got %s", expectedTime, optionTime)
	}

	if err := option.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Restart the node.

	ctx := proVM.ctx
	proVM = New(
		coreVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		pTestCert.PrivateKey.(crypto.Signer),
		pTestCert.Leaf,
	)

	coreVM.InitializeF = func(
		context.Context,
		*snow.Context,
		manager.Manager,
		[]byte,
		[]byte,
		[]byte,
		chan<- common.Message,
		[]*common.Fx,
		common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreOracleBlk.opts[0].ID(), nil
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	err = proVM.Initialize(
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	statefulOptionBlock, err := proVM.ParseBlock(context.Background(), option.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if status := statefulOptionBlock.Status(); status != choices.Accepted {
		t.Fatalf("wrong status returned expected %s got %s", choices.Accepted, status)
	}

	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		t.Fatal("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		t.Fatal("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	if optionTime := statefulOptionBlock.Timestamp(); !optionTime.Equal(expectedTime) {
		t.Fatalf("wrong time returned expected %s got %s", expectedTime, optionTime)
	}
}
