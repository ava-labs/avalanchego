// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"errors"
	"testing"
	"time"

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
		ParentV:    gBlock,
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
		ParentV:    xBlock,
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
