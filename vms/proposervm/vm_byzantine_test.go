// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"encoding/hex"
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

func TestGetBlock_MutatedSignature(t *testing.T) {
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)

	// Make sure that we will be sampled to perform the proposals.
	valState.GetValidatorSetF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[proVM.ctx.NodeID] = uint64(10)
		return res, nil
	}

	proVM.Set(coreGenBlk.Timestamp())

	// Create valid core blocks to build our chain on.
	coreBlk0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}

	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk0.ID(),
		HeightV:    coreBlk0.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}

	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// Build the first proposal block
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk0, nil }

	builtBlk0, err := proVM.BuildBlock()
	if err != nil {
		t.Fatalf("could not build post fork block %s", err)
	}

	if err := builtBlk0.Verify(); err != nil {
		t.Fatalf("failed to verify newly created block %s", err)
	}

	if err := proVM.SetPreference(builtBlk0.ID()); err != nil {
		t.Fatal(err)
	}

	// The second propsal block will need to be signed because the timestamp
	// hasn't moved forward

	// Craft what would be the next block, but with an invalid signature:
	// ID: 2R3Uz98YmxHUJARWv6suApPdAbbZ7X7ipat1gZuZNNhC5wPwJW
	// Valid Bytes: 000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000020098147a41989d8626f63d0966b39376143e45ea6e21b62761a115660d88db9cba37be71d1e1153e7546eb075749122449f2f3f5984e51773f082700d847334da35babe72a66e5a49c9a96cd763bdd94258263ae92d30da65d7c606482d0afe9f4f884f4f6c33d6d8e1c0c71061244ebec6a9dbb9b78bfbb71dec572aa0c0d8e532bf779457e05412b75acf12f35c75917a3eda302aaa27c3090e93bf5de0c3e30968cf8ba025b91962118bbdb6612bf682ba6e87ae6cd1a5034c89559b76af870395dc17ec592e9dbb185633aa1604f8d648f82142a2d1a4dabd91f816b34e73120a70d061e64e6da62ba434fd0cdf7296aa67fd5e0432ef8cee67c1b59aee91c99288c17a8511d96ba7339fb4ae5da453289aa7a9fab00d37035accae24eef0eaf517148e67bdc76adaac2429508d642df3033ad6c9e3fb53057244c1295f2ed3ac66731f77178fccb7cc4fd40778ccb061e5d53cd0669371d8d355a4a733078a9072835b5564a52a50f5db8525d2ee00466124a8d40d9959281b86a789bd0769f3fb0deb89f0eb9cfe036ff8a0011f52ca551c30202f46680acfa656ccf32a4e8a7121ef52442128409dc40d21d61205839170c7b022f573c2cfdaa362df22e708e7572b9b77f4fb20fe56b122bcb003566e20caef289f9d7992c2f1ad0c8366f71e8889390e0d14e2e76c56b515933b0c337ac6bfcf76d33e2ba50cb62eb71
	// Invalid Bytes: 000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000000101
	invalidBlkBytesHex := "000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000000101"
	invalidBlkBytes, err := hex.DecodeString(invalidBlkBytesHex)
	if err != nil {
		t.Fatal(err)
	}

	invalidBlk, err := proVM.ParseBlock(invalidBlkBytes)
	if err != nil {
		// Not being able to parse an invalid block is fine.
		t.Skip(err)
	}

	if err := invalidBlk.Verify(); err == nil {
		t.Fatalf("verified block without valid signature")
	}

	// Note that the invalidBlk.ID() is the same as the correct blk ID because
	// the signature isn't part of the blk ID.
	blkID, err := ids.FromString("2R3Uz98YmxHUJARWv6suApPdAbbZ7X7ipat1gZuZNNhC5wPwJW")
	if err != nil {
		t.Fatal(err)
	}

	if blkID != invalidBlk.ID() {
		t.Fatalf("unexpected block ID; expected = %s , got = %s", blkID, invalidBlk.ID())
	}

	// GetBlock shouldn't really be able to succeed, as we don't have a valid
	// representation of [blkID]
	fetchedBlk, err := proVM.GetBlock(blkID)
	if err != nil {
		t.Skip(err)
	}

	// GetBlock returned, so it must have somehow gotten a valid representation
	// of [blkID].
	if err := fetchedBlk.Verify(); err != nil {
		t.Fatalf("GetBlock returned an invalid block when the ID represented a potentially valid block: %s", err)
	}
}
