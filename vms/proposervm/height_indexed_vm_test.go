package proposervm

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	snowmanVMs "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/stretchr/testify/assert"
)

func TestHeightBlockIndexPostFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of post fork blocks
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
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
		proBlks   = make(map[ids.ID]PostForkBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		// build wrapping post fork block
		statelessChild, err := block.BuildUnsigned(
			lastProBlk.ID(), // parentID
			lastProBlk.Timestamp().Add(proposer.MaxDelay), // timestamp
			uint64(0), // pChainHeight
			lastInnerBlk.Bytes(),
		)
		assert.NoError(err)
		postForkBlk := &postForkBlock{
			SignedBlock: statelessChild,
			postForkCommonComponents: postForkCommonComponents{
				innerBlk: lastInnerBlk,
				status:   choices.Accepted,
			},
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	hIndex := &heightIndexer{
		innerHVM: &snowmanVMs.TestHeightIndexedVM{
			HeightIndexingEnabledF: func() bool { return true },
		},
		log:        logging.NoLog{},
		indexState: state.NewHeightIndex(dbMan.Current().Database),

		lastAcceptedPostForkBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		lastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		getPostForkBlkF: func(blkID ids.ID) (PostForkBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		getInnerBlkF: func(blkID ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		dbCommitF: func() error { return nil },
	}

	// height index is empty at start
	assert.True(hIndex.latestPreForkHeight == 0)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}

	// show that height index should be rebuild and it is
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.True(doRepair)
	assert.True(startBlkID == lastProBlk.ID())
	assert.NoError(hIndex.doRepair(startBlkID))

	// check that height index is fully built
	assert.True(hIndex.latestPreForkHeight == 0)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.NoError(err)
	}

	// check that height index wont' be rebuild anymore
	doRepair, _, err = hIndex.shouldRepair()
	assert.NoError(err)
	assert.False(doRepair)
}

func TestHeightBlockIndexPreFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of pre fork blocks
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

	var (
		blkNumber    = uint64(10)
		prevInnerBlk = snowman.Block(innerGenBlk)
		innerBlks    = make(map[ids.ID]snowman.Block)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	for blkHeight := uint64(1); blkHeight <= blkNumber; blkHeight++ {
		// build inner block
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		prevInnerBlk = lastInnerBlk
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	hIndex := &heightIndexer{
		innerHVM: &snowmanVMs.TestHeightIndexedVM{
			HeightIndexingEnabledF: func() bool { return true },
		},
		log:        logging.NoLog{},
		indexState: state.NewHeightIndex(dbMan.Current().Database),

		lastAcceptedPostForkBlkIDF: func() (ids.ID, error) { return ids.Empty, database.ErrNotFound },
		lastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		getPostForkBlkF:            func(blkID ids.ID) (PostForkBlock, error) { return nil, database.ErrNotFound },
		getInnerBlkF: func(blkID ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		dbCommitF: func() error { return nil },
	}

	// height index is empty at start
	assert.True(hIndex.latestPreForkHeight == 0)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}

	// with preFork only blocks there is nothing to rebuild
	doRepair, _, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.False(doRepair)
}

func TestHeightBlockIndexAcrossFork(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of pre fork and post fork blocks
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
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
		proBlks   = make(map[ids.ID]PostForkBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	// build preFork blocks
	for blkHeight := uint64(1); blkHeight <= forkHeight; blkHeight++ {
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		prevInnerBlk = lastInnerBlk
	}

	// build postFork blocks
	for blkHeight := forkHeight + 1; blkHeight <= blkNumber; blkHeight++ {
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		statelessChild, err := block.BuildUnsigned(
			lastProBlk.ID(),
			lastProBlk.Timestamp().Add(proposer.MaxDelay),
			uint64(0),
			lastInnerBlk.Bytes(),
		)
		assert.NoError(err)
		postForkBlk := &postForkBlock{
			SignedBlock: statelessChild,
			postForkCommonComponents: postForkCommonComponents{
				innerBlk: lastInnerBlk,
				status:   choices.Accepted,
			},
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	hIndex := &heightIndexer{
		innerHVM: &snowmanVMs.TestHeightIndexedVM{
			HeightIndexingEnabledF: func() bool { return true },
		},
		log:        logging.NoLog{},
		indexState: state.NewHeightIndex(dbMan.Current().Database),

		lastAcceptedPostForkBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		lastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		getPostForkBlkF: func(blkID ids.ID) (PostForkBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		getInnerBlkF: func(blkID ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		dbCommitF: func() error { return nil },
	}

	// height index is empty at start
	assert.True(hIndex.latestPreForkHeight == 0)
	for height := uint64(1); height <= blkNumber; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}

	// show that height index should be rebuild and it is
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.NoError(err)
	assert.True(doRepair)
	assert.True(startBlkID == lastProBlk.ID())
	assert.NoError(hIndex.doRepair(startBlkID))

	// check that height index is fully built
	assert.True(hIndex.latestPreForkHeight == forkHeight)
	for height := uint64(0); height <= forkHeight; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.Error(err, database.ErrNotFound)
	}
	for height := forkHeight + 1; height <= blkNumber; height++ {
		_, err := hIndex.indexState.GetBlkIDByHeight(height)
		assert.NoError(err)
	}

	// check that height index wont' be rebuild anymore
	doRepair, _, err = hIndex.shouldRepair()
	assert.NoError(err)
	assert.False(doRepair)
}

func TestHeightBlockIndexResumefromCheckPoint(t *testing.T) {
	assert := assert.New(t)

	// Build a chain of pre fork and post fork blocks
	innerGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
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
		proBlks   = make(map[ids.ID]PostForkBlock)
	)
	innerBlks[innerGenBlk.ID()] = innerGenBlk

	// build preFork blocks
	for blkHeight := uint64(1); blkHeight <= forkHeight; blkHeight++ {
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		prevInnerBlk = lastInnerBlk
	}

	// build postFork blocks
	for blkHeight := forkHeight + 1; blkHeight <= blkNumber; blkHeight++ {
		lastInnerBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			BytesV:     []byte{uint8(blkHeight)},
			ParentV:    prevInnerBlk.ID(),
			HeightV:    blkHeight,
			TimestampV: prevInnerBlk.Timestamp().Add(proposer.MaxDelay),
		}
		innerBlks[lastInnerBlk.ID()] = lastInnerBlk

		statelessChild, err := block.BuildUnsigned(
			lastProBlk.ID(),
			lastProBlk.Timestamp().Add(proposer.MaxDelay),
			uint64(0),
			lastInnerBlk.Bytes(),
		)
		assert.NoError(err)
		postForkBlk := &postForkBlock{
			SignedBlock: statelessChild,
			postForkCommonComponents: postForkCommonComponents{
				innerBlk: lastInnerBlk,
				status:   choices.Accepted,
			},
		}
		proBlks[postForkBlk.ID()] = postForkBlk

		lastProBlk = postForkBlk
		prevInnerBlk = lastInnerBlk
	}

	dbMan := manager.NewMemDB(version.DefaultVersion1_0_0)
	hIndex := &heightIndexer{
		innerHVM: &snowmanVMs.TestHeightIndexedVM{
			HeightIndexingEnabledF: func() bool { return true },
		},
		log:        logging.NoLog{},
		indexState: state.NewHeightIndex(dbMan.Current().Database),

		lastAcceptedPostForkBlkIDF: func() (ids.ID, error) { return lastProBlk.ID(), nil },
		lastAcceptedInnerBlkIDF:    func() (ids.ID, error) { return prevInnerBlk.ID(), nil },
		getPostForkBlkF: func(blkID ids.ID) (PostForkBlock, error) {
			blk, found := proBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		getInnerBlkF: func(blkID ids.ID) (snowman.Block, error) {
			blk, found := innerBlks[blkID]
			if !found {
				return nil, database.ErrNotFound
			}
			return blk, nil
		},
		dbCommitF: func() error { return nil },
	}

	// with no checkpoints repair starts from last accepted block
	doRepair, startBlkID, err := hIndex.shouldRepair()
	assert.True(doRepair)
	assert.NoError(err)
	assert.True(startBlkID == lastProBlk.ID())

	// if an intermediate block is checkpointed, repair will start from it
	rndPostForkHeight := rand.Intn(int(blkNumber-forkHeight)) + int(forkHeight) // #nosec G404
	for _, blk := range proBlks {
		if blk.Height() != uint64(rndPostForkHeight) {
			continue // not the blk we are looking for
		}

		checkpointBlk := blk
		assert.NoError(hIndex.indexState.SetRepairCheckpoint(checkpointBlk.ID()))

		doRepair, startBlkID, err := hIndex.shouldRepair()
		assert.True(doRepair)
		assert.NoError(err)
		assert.True(startBlkID == checkpointBlk.ID())
	}
}
