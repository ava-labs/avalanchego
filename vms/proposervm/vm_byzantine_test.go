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
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

// Ensure that a byzantine node issuing an invalid PreForkBlock (Y) when the
// parent block (X) is issued into a PostForkBlock (A) will be marked as invalid
// correctly.
//     G
//   / |
// A - X
//     |
//     Y
func TestInvalidByzantineProposerParent(t *testing.T) {
	forkTime := time.Unix(0, 0) // enable ProBlks
	coreVM, _, proVM, gBlock, _ := initTestProposerVM(t, forkTime, 0)

	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }

	aBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}

	coreVM.BuildBlockF = nil

	if err := aBlock.Verify(); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	if err := aBlock.Accept(); err != nil {
		t.Fatalf("could not accept valid block due to %s", err)
	}

	yBlockBytes := []byte{2}
	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     yBlockBytes,
		ParentV:    xBlock.ID(),
		HeightV:    xBlock.Height() + 1,
		TimestampV: xBlock.Timestamp().Add(proposer.MaxDelay),
	}

	coreVM.ParseBlockF = func(blockBytes []byte) (snowman.Block, error) {
		if !bytes.Equal(blockBytes, yBlockBytes) {
			return nil, errUnknownBlock
		}
		return yBlock, nil
	}

	parsedBlock, err := proVM.ParseBlock(yBlockBytes)
	if err != nil {
		// If there was an error parsing, then this is fine.
		return
	}

	// If there wasn't an error parsing - verify must return an error
	if err := parsedBlock.Verify(); err == nil {
		t.Fatal("should have marked the parsed block as invalid")
	}
}

// Ensure that a byzantine node issuing an invalid PreForkBlock (Y or Z) when
// the parent block (X) is issued into a PostForkBlock (A) will be marked as
// invalid correctly.
//     G
//   / |
// A - X
//    / \
//   Y   Z
func TestInvalidByzantineProposerOracleParent(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	xBlockID := ids.GenerateTestID()
	xBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     xBlockID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case xBlock.ID():
			return xBlock, nil
		case xBlock.opts[0].ID():
			return xBlock.opts[0], nil
		case xBlock.opts[1].ID():
			return xBlock.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(b, xBlock.opts[0].Bytes()):
			return xBlock.opts[0], nil
		case bytes.Equal(b, xBlock.opts[1].Bytes()):
			return xBlock.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	aBlockIntf, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	aBlock, ok := aBlockIntf.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}

	opts, err := aBlock.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := aBlock.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := opts[0].Verify(); err != nil {
		t.Fatal(err)
	}
	if err := opts[1].Verify(); err != nil {
		t.Fatal(err)
	}

	yBlock, err := proVM.ParseBlock(xBlock.opts[0].Bytes())
	if err != nil {
		// It's okay for this block not to be parsed
		return
	}
	if err := yBlock.Verify(); err == nil {
		t.Fatal("unexpectedly passed block verification")
	}

	if err := aBlock.Accept(); err != nil {
		t.Fatal(err)
	}

	if err := yBlock.Verify(); err == nil {
		t.Fatal("unexpectedly passed block verification")
	}
}

// Ensure that a byzantine node issuing an invalid PostForkBlock (B) when the
// parent block (X) is issued into a PostForkBlock (A) will be marked as invalid
// correctly.
//     G
//   / |
// A - X
//   / |
// B - Y
func TestInvalidByzantineProposerPreForkParent(t *testing.T) {
	forkTime := time.Unix(0, 0) // enable ProBlks
	coreVM, _, proVM, gBlock, _ := initTestProposerVM(t, forkTime, 0)

	xBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    gBlock.ID(),
		HeightV:    gBlock.Height() + 1,
		TimestampV: gBlock.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }

	aBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("proposerVM could not build block due to %s", err)
	}

	coreVM.BuildBlockF = nil

	yBlockBytes := []byte{2}
	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     yBlockBytes,
		ParentV:    xBlock.ID(),
		HeightV:    xBlock.Height() + 1,
		TimestampV: xBlock.Timestamp().Add(proposer.MaxDelay),
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case gBlock.ID():
			return gBlock, nil
		case xBlock.ID():
			return xBlock, nil
		case yBlock.ID():
			return yBlock, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(blockBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blockBytes, gBlock.Bytes()):
			return gBlock, nil
		case bytes.Equal(blockBytes, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(blockBytes, yBlock.Bytes()):
			return yBlock, nil
		default:
			return nil, errUnknownBlock
		}
	}

	bStatelessBlock, err := block.BuildUnsigned(
		xBlock.ID(),
		yBlock.Timestamp(),
		0,
		yBlockBytes,
	)
	if err != nil {
		t.Fatal(err)
	}

	bBlock, err := proVM.ParseBlock(bStatelessBlock.Bytes())
	if err != nil {
		// If there was an error parsing, then this is fine.
		return
	}

	if err := aBlock.Verify(); err != nil {
		t.Fatalf("could not verify valid block due to %s", err)
	}

	// If there wasn't an error parsing - verify must return an error
	if err := bBlock.Verify(); err == nil {
		t.Fatal("should have marked the parsed block as invalid")
	}

	if err := aBlock.Accept(); err != nil {
		t.Fatalf("could not accept valid block due to %s", err)
	}

	// If there wasn't an error parsing - verify must return an error
	if err := bBlock.Verify(); err == nil {
		t.Fatal("should have marked the parsed block as invalid")
	}
}

// Ensure that a byzantine node issuing an invalid OptionBlock (B) which
// contains core block (Y) whose parent (G) doesn't match (B)'s parent (A)'s
// inner block (X) will be marked as invalid correctly.
//     G
//   / | \
// A - X  |
// |     /
// B - Y
func TestBlockVerify_PostForkOption_FaultyParent(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	xBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreGenBlk.ID(), // valid block should reference xBlock
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreGenBlk.ID(), // valid block should reference xBlock
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case xBlock.ID():
			return xBlock, nil
		case xBlock.opts[0].ID():
			return xBlock.opts[0], nil
		case xBlock.opts[1].ID():
			return xBlock.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(b, xBlock.opts[0].Bytes()):
			return xBlock.opts[0], nil
		case bytes.Equal(b, xBlock.opts[1].Bytes()):
			return xBlock.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	aBlockIntf, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	aBlock, ok := aBlockIntf.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := aBlock.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := aBlock.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := opts[0].Verify(); err == nil {
		t.Fatal("option 0 has invalid parent, should not verify")
	}
	if err := opts[1].Verify(); err == nil {
		t.Fatal("option 1 has invalid parent, should not verify")
	}
}

//   ,--G ----.
//  /    \     \
// A(X)  B(Y)  C(Z)
// | \_ /_____/
// |\  /   |
// | \/    |
// O2 O1   O3
//
// O1.parent = B (non-Oracle), O1.inner = first option of X (invalid)
// O2.parent = A (original), O2.inner = first option of X (valid)
// O3.parent = C (Oracle), O3.inner = first option of X (invalid parent)
func TestBlockVerify_InvalidPostForkOption(t *testing.T) {
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	// create an Oracle pre-fork block X
	xBlockID := ids.GenerateTestID()
	xBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     xBlockID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    xBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	xInnerOptions, err := xBlock.Options()
	if err != nil {
		t.Fatal(err)
	}
	xInnerOption := xInnerOptions[0]

	// create a non-Oracle pre-fork block Y
	yBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}

	ySlb, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		uint64(2000),
		yBlock.Bytes(),
	)
	if err != nil {
		t.Fatalf("fail to manually build a block due to %s", err)
	}

	// create post-fork block B from Y
	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
			status:   choices.Processing,
		},
	}

	if err = bBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	// generate O1
	statelessOuterOption, err := block.BuildOption(
		bBlock.ID(),
		xInnerOption.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	outerOption := &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
			status:   xInnerOption.Status(),
		},
	}

	if err := outerOption.Verify(); err != errUnexpectedBlockType {
		t.Fatal(err)
	}

	// generate A from X and O2
	coreVM.BuildBlockF = func() (snowman.Block, error) { return xBlock, nil }
	aBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	coreVM.BuildBlockF = nil
	if err := aBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	statelessOuterOption, err = block.BuildOption(
		aBlock.ID(),
		xInnerOption.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	outerOption = &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
			status:   xInnerOption.Status(),
		},
	}

	if err := outerOption.Verify(); err != nil {
		t.Fatal(err)
	}

	// create an Oracle pre-fork block Z
	// create post-fork block B from Y
	zBlockID := ids.GenerateTestID()
	zBlock := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     zBlockID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    zBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    zBlockID,
				TimestampV: coreGenBlk.Timestamp(),
			},
		},
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return zBlock, nil }
	cBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	coreVM.BuildBlockF = nil
	if err := cBlock.Verify(); err != nil {
		t.Fatal(err)
	}

	// generate O3
	statelessOuterOption, err = block.BuildOption(
		cBlock.ID(),
		xInnerOption.Bytes(),
	)
	if err != nil {
		t.Fatal(err)
	}

	outerOption = &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
			status:   xInnerOption.Status(),
		},
	}

	if err := outerOption.Verify(); err != errInnerParentMismatch {
		t.Fatal(err)
	}
}
