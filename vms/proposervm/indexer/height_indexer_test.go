// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/stretchr/testify/assert"
)

var (
	genesisUnixTimestamp int64 = 1000
	genesisTimestamp           = time.Unix(genesisUnixTimestamp, 0)
)

func TestHeightBlockIndexPostFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of wrapping blocks, representing post fork blocks
	innerBlkID := ids.Empty.Prefix(0)
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     innerBlkID,
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	var (
		blkNumber = uint64(10)

		prevInnerBlk = snowman.Block(innerGenBlk)
		lastProBlk   = snowman.Block(innerGenBlk)

		innerBlks = make(map[ids.ID]snowman.Block)
		proBlks   = make(map[ids.ID]WrappingBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		// build wrapping post fork block
		wrappingID := ids.Empty.Prefix(blkHeight + blkNumber + 1)
		postForkBlk := &TestWrappingBlock{
			TestBlock: &snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     wrappingID,
					StatusV: choices.Accepted,
				},
				BytesV:  wrappingID[:],
				ParentV: lastProBlk.ID(),
				HeightV: lastInnerBlk.Height(),
			},
			innerBlk: lastInnerBlk,
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	blkSrv := &TestBlockServer{
		CantLastAcceptedWrappingBlkID: true,
		CantLastAcceptedInnerBlkID:    true,
		CantGetWrappingBlk:            true,
		CantGetInnerBlk:               true,

		LastAcceptedWrappingBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		LastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		GetWrappingBlkF: func(blkID ids.ID) (WrappingBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		GetInnerBlkF: func(id ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[id]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	storedState := state.NewHeightIndex(dbMan.Current().Database)
	hIndex := newHeightIndexer(blkSrv,
		logging.NoLog{},
		storedState,
	)
	hIndex.commitMaxSize = 0 // commit each block

	// show that height index should be rebuild and it is
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.True(doRepair)
	assert.True(startBlkID == lastProBlk.ID())
	assert.NoError(hIndex.doRepair(startBlkID))
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on doRepair caller

	// check that height index is fully built
	loadedForkHeight, err := storedState.GetForkHeight()
	assert.NoError(err)
	assert.True(loadedForkHeight == 1)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := storedState.GetBlockIDAtHeight(height)
		assert.NoError(err)
	}

	// check that height index wont' be rebuild anymore
	assert.False(hIndex.shouldRepair())
	assert.True(hIndex.IsRepaired())
}

func TestHeightBlockIndexPreFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of non-wrapping blocks, representing pre fork blocks
	innerBlkID := ids.Empty.Prefix(0)
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     innerBlkID,
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	var (
		blkNumber = uint64(10)

		prevInnerBlk = snowman.Block(innerGenBlk)
		innerBlks    = make(map[ids.ID]snowman.Block)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk
		prevInnerBlk = lastInnerBlk
	}

	blkSrv := &TestBlockServer{
		CantLastAcceptedWrappingBlkID: true,
		CantLastAcceptedInnerBlkID:    true,
		CantGetWrappingBlk:            true,
		CantGetInnerBlk:               true,

		LastAcceptedWrappingBlkIDF: func() (ids.ID, error) {
			// all blocks are pre-fork
			return ids.Empty, database.ErrNotFound
		},
		LastAcceptedInnerBlkIDF: func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		GetWrappingBlkF: func(blkID ids.ID) (WrappingBlock, error) {
			// all blocks are pre-fork
			return nil, database.ErrNotFound
		},
		GetInnerBlkF: func(id ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[id]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	storedState := state.NewHeightIndex(dbMan.Current().Database)
	hIndex := newHeightIndexer(blkSrv,
		logging.NoLog{},
		storedState,
	)
	hIndex.commitMaxSize = 0 // commit each block

	// with preFork only blocks there is nothing to rebuild
	doRepair, _, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.False(doRepair)
	assert.True(hIndex.IsRepaired())
}

func TestHeightBlockIndexAcrossFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of non-wrapping and wrapping blocks, representing pre and post fork blocks
	innerBlkID := ids.Empty.Prefix(0)
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     innerBlkID,
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	var (
		blkNumber  = uint64(10)
		forkHeight = blkNumber / 2

		prevInnerBlk = snowman.Block(innerGenBlk)
		lastProBlk   = snowman.Block(innerGenBlk)

		innerBlks = make(map[ids.ID]snowman.Block)
		proBlks   = make(map[ids.ID]WrappingBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight < forkHeight; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk
		prevInnerBlk = lastInnerBlk
	}

	for blkHeight := forkHeight; blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		// build wrapping post fork block
		wrappingID := ids.Empty.Prefix(blkHeight + blkNumber + 1)
		postForkBlk := &TestWrappingBlock{
			TestBlock: &snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     wrappingID,
					StatusV: choices.Accepted,
				},
				BytesV:  wrappingID[:],
				ParentV: lastProBlk.ID(),
				HeightV: lastInnerBlk.Height(),
			},
			innerBlk: lastInnerBlk,
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	blkSrv := &TestBlockServer{
		CantLastAcceptedWrappingBlkID: true,
		CantLastAcceptedInnerBlkID:    true,
		CantGetWrappingBlk:            true,
		CantGetInnerBlk:               true,

		LastAcceptedWrappingBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		LastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		GetWrappingBlkF: func(blkID ids.ID) (WrappingBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		GetInnerBlkF: func(id ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[id]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	storedState := state.NewHeightIndex(dbMan.Current().Database)
	hIndex := newHeightIndexer(blkSrv,
		logging.NoLog{},
		storedState,
	)
	hIndex.commitMaxSize = 0 // commit each block

	// show that height index should be rebuild and it is
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on shouldRepair caller
	assert.True(doRepair)
	assert.True(startBlkID == lastProBlk.ID())
	assert.NoError(hIndex.doRepair(startBlkID))
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on doRepair caller

	// check that height index is fully built
	loadedForkHeight, err := storedState.GetForkHeight()
	assert.NoError(err)
	assert.True(loadedForkHeight == forkHeight)
	for height := uint64(0); height < forkHeight; height++ {
		_, err := storedState.GetBlockIDAtHeight(height)
		assert.Error(err, database.ErrNotFound)
	}
	for height := forkHeight; height <= blkNumber; height++ {
		_, err := storedState.GetBlockIDAtHeight(height)
		assert.NoError(err)
	}

	// check that height index wont' be rebuild anymore
	assert.False(hIndex.shouldRepair())
	assert.True(hIndex.IsRepaired())
}

func TestHeightBlockIndexResumeFromCheckPoint(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of non-wrapping and wrapping blocks, representing pre and post fork blocks
	innerBlkID := ids.Empty.Prefix(0)
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     innerBlkID,
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	var (
		blkNumber  = uint64(10)
		forkHeight = blkNumber / 2

		prevInnerBlk = snowman.Block(innerGenBlk)
		lastProBlk   = snowman.Block(innerGenBlk)

		innerBlks = make(map[ids.ID]snowman.Block)
		proBlks   = make(map[ids.ID]WrappingBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight < forkHeight; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk
		prevInnerBlk = lastInnerBlk
	}

	for blkHeight := forkHeight; blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		innerBlkID := ids.Empty.Prefix(blkHeight)
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     innerBlkID,
				StatusV: choices.Accepted,
			},
			BytesV:  []byte{uint8(blkHeight)},
			ParentV: prevInnerBlk.ID(),
			HeightV: blkHeight,
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		// build wrapping post fork block
		wrappingID := ids.Empty.Prefix(blkHeight + blkNumber + 1)
		postForkBlk := &TestWrappingBlock{
			TestBlock: &snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     wrappingID,
					StatusV: choices.Accepted,
				},
				BytesV:  wrappingID[:],
				ParentV: lastProBlk.ID(),
				HeightV: lastInnerBlk.Height(),
			},
			innerBlk: lastInnerBlk,
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	blkSrv := &TestBlockServer{
		CantLastAcceptedWrappingBlkID: true,
		CantLastAcceptedInnerBlkID:    true,
		CantGetWrappingBlk:            true,
		CantGetInnerBlk:               true,

		LastAcceptedWrappingBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		LastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		GetWrappingBlkF: func(blkID ids.ID) (WrappingBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		GetInnerBlkF: func(id ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[id]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	storedState := state.NewHeightIndex(dbMan.Current().Database)
	hIndex := newHeightIndexer(blkSrv,
		logging.NoLog{},
		storedState,
	)
	hIndex.commitMaxSize = 0 // commit each block

	// with no checkpoints repair starts from last accepted block
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on shouldRepair caller
	assert.True(doRepair)
	assert.NoError(err)
	assert.True(startBlkID == lastProBlk.ID())

	// pick a random block in the chain and checkpoint it;...
	rndPostForkHeight := rand.Intn(int(blkNumber-forkHeight)) + int(forkHeight) // #nosec G404
	var checkpointBlk WrappingBlock
	for _, blk := range proBlks {
		if blk.Height() != uint64(rndPostForkHeight) {
			continue // not the blk we are looking for
		}

		checkpointBlk = blk
		assert.NoError(hIndex.indexState.SetCheckpoint(checkpointBlk.ID()))
		break
	}

	// ...show that repair starts from the checkpoint
	doRepair, startBlkID, err = hIndex.shouldRepair()
	assert.True(doRepair)
	assert.NoError(err)
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on shouldRepair caller
	assert.True(startBlkID == checkpointBlk.ID())
	assert.False(hIndex.IsRepaired())

	// perform repair and show index is built
	assert.NoError(hIndex.doRepair(startBlkID))
	assert.NoError(hIndex.batch.Write()) // batch write responsibility is on doRepair caller

	// check that height index is fully built
	loadedForkHeight, err := storedState.GetForkHeight()
	assert.NoError(err)
	assert.True(loadedForkHeight == forkHeight)
	for height := forkHeight; height <= checkpointBlk.Height(); height++ {
		_, err := storedState.GetBlockIDAtHeight(height)
		assert.NoError(err)
	}
}
