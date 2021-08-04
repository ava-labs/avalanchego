// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var errUnknownBlock = errors.New("unknown block")

// Ensure that a byzantine node issuing an invalid block (Y) will not be
// verified correctly.
//     G
//   / |
// A - X
//     |
//     Y
func TestInvalidByzantineProposerParent(t *testing.T) {
	forkTime := time.Unix(0, 0) // enable ProBlks
	coreVM, _, proVM, gBlock := initTestProposerVM(t, forkTime)

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

func TestBlockVerify_PostForkOption_FaultyParent(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Time{})
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
			ParentV:    coreGenBlk.ID(), // valid block should reference oracleCoreBlk
			TimestampV: coreGenBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    coreGenBlk.ID(), // valid block should reference oracleCoreBlk
			TimestampV: coreGenBlk.Timestamp(),
		},
	}

	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.CantGetBlock = true
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
			return nil, fmt.Errorf("Unknown block")
		}
	}

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
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

	// ... and verify them
	if err := opts[0].Verify(); err == nil {
		t.Fatal("option 0 has invalid parent, should not verify")
	}
	if err := opts[1].Verify(); err == nil {
		t.Fatal("option 1 has invalid parent, should not verify")
	}
}
