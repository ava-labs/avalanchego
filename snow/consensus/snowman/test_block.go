// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"sort"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// TestBlock is a useful test block
type TestBlock struct {
	choices.TestDecidable

	ParentV Block
	HeightV uint64
	VerifyV error
	BytesV  []byte
}

// Parent implements the Block interface
func (b *TestBlock) Parent() Block { return b.ParentV }

// Height returns the height of the block
func (b *TestBlock) Height() uint64 { return b.HeightV }

// Verify implements the Block interface
func (b *TestBlock) Verify() error { return b.VerifyV }

// Bytes implements the Block interface
func (b *TestBlock) Bytes() []byte { return b.BytesV }

// SetStatus sets the status of the Block. Implements the BlockWrapper interface.
func (b *TestBlock) SetStatus(status choices.Status) { b.TestDecidable.StatusV = status }

type sortBlocks []*TestBlock

func (sb sortBlocks) Less(i, j int) bool { return sb[i].HeightV < sb[j].HeightV }
func (sb sortBlocks) Len() int           { return len(sb) }
func (sb sortBlocks) Swap(i, j int)      { sb[j], sb[i] = sb[i], sb[j] }

// SortTestBlocks sorts the array of blocks by height
func SortTestBlocks(blocks []*TestBlock) { sort.Sort(sortBlocks(blocks)) }

// NewTestBlock returns a new test block with height, bytes, and ID derived from [i]
// and using [parent] as the parent block
func NewTestBlock(i uint64, parent *TestBlock) *TestBlock {
	b := []byte{byte(i)}
	id := hashing.ComputeHash256Array(b)
	return &TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     id,
			StatusV: choices.Unknown,
		},
		HeightV: i,
		ParentV: parent,
		BytesV:  b,
	}
}

// NewTestBlocks generates [numBlocks] consecutive blocks starting
// at height 0.
func NewTestBlocks(numBlocks uint64, parent *TestBlock) []*TestBlock {
	blks := make([]*TestBlock, 0, numBlocks)
	var startHeight uint64
	if parent != nil {
		startHeight = parent.HeightV + 1
	}
	for i := startHeight; i < numBlocks; i++ {
		blks = append(blks, NewTestBlock(i, parent))
		parent = blks[len(blks)-1]
	}

	return blks
}
