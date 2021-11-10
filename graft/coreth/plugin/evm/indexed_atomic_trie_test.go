package evm

import (
	"github.com/ava-labs/coreth/fastsync/facades"
	"github.com/ethereum/go-ethereum/common"
)

type atomicBlockFacade struct {
	num     uint64
	root    common.Hash
	extData []byte
}

func newAtomicBlockFacade(num uint64, root common.Hash, extData []byte) *atomicBlockFacade {
	return &atomicBlockFacade{num: num, root: root, extData: extData}
}

func (a atomicBlockFacade) NumberU64() uint64 {
	return a.num
}

func (a atomicBlockFacade) Root() common.Hash {
	return a.root
}

func (a atomicBlockFacade) ExtData() []byte {
	return a.extData
}

type testChainFacade struct {
	lastAcceptedBlock  facades.BlockFacade
	getBlockByNumberFn func(uint64) facades.BlockFacade
}

func newTestChainFacade(lastAcceptedBlock facades.BlockFacade, getBlockByNumberFn func(uint64) facades.BlockFacade) *testChainFacade {
	return &testChainFacade{lastAcceptedBlock: lastAcceptedBlock, getBlockByNumberFn: getBlockByNumberFn}
}

func (t testChainFacade) LastAcceptedBlock() facades.BlockFacade {
	return t.lastAcceptedBlock
}

func (t testChainFacade) GetBlockByNumber(num uint64) facades.BlockFacade {
	return t.getBlockByNumberFn(num)
}

/*const testCommitInterval = 100

func Test_IndexerWriteAndRead(t *testing.T) {
	db := memorydb.New()
	indexer, err := NewIndexedAtomicTrie(db)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		indexer.commitHeightInterval = testCommitInterval
		indexer.initialised.Store(true)
	}

	// ensure invalid height commits are not possible
	_, err = indexer.Index(999, nil)
	assert.Error(t, err)
	assert.Equal(t, 0, db.Len())

	// ensure last is uninitialised
	hash, lastCommittedIndexHeight, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), lastCommittedIndexHeight)
	assert.Equal(t, common.Hash{}, hash)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	lastCommittedBlockHash := common.Hash{}
	totalAtomicRequests := 0
	// process blocks and insert them into the indexer
	maxBlocks := uint64(125)
	entriesPerBlock := 2
	for height := uint64(0); height < maxBlocks; height++ {
		atomicRequests := make(map[ids.ID]*atomic.Requests)
		for j := 0; j < entriesPerBlock; j++ {
			blockchainID := ids.GenerateTestID()
			req := atomic.Requests{
				PutRequests: []*atomic.Element{
					{
						Key:   utils.RandomBytes(16),
						Value: utils.RandomBytes(24),
						Traits: [][]byte{
							utils.RandomBytes(32),
							utils.RandomBytes(32),
						},
					},
				},
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			}
			atomicRequests[blockchainID] = &req
			totalAtomicRequests++
		}
		hash, err := indexer.Index(height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			assert.NotEqual(t, common.Hash{}, hash)
			blockRootMap[height] = hash
			lastCommittedBlockHeight = height
			lastCommittedBlockHash = hash
		}
	}

	assert.NotEqual(t, 0, lastCommittedBlockHeight)
	assert.Len(t, blockRootMap, 2)

	height, initialized, err := indexer.Height()
	assert.NoError(t, err)
	assert.True(t, initialized)
	assert.EqualValues(t, maxBlocks-1, height)

	hash, lastCommittedIndexHeight, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, lastCommittedBlockHeight, lastCommittedIndexHeight)
	assert.Equal(t, lastCommittedBlockHash, hash)

	iter, err := indexer.Iterator(hash)
	blockSet := make(map[uint64]struct{})
	assert.NoError(t, err)
	for iter.Next() {
		blockSet[iter.BlockNumber()] = struct{}{}
	}
	assert.Nil(t, iter.Errors())

	// we expect 101 because we got blocks 0-100 (inclusive)
	// and the Iterate is from the last committed hash which
	// is at block 100
	assert.EqualValues(t, 101, len(blockSet), "expect 100 blocks in the set")
}

func Test_IndexerInitializeFromGenesis(t *testing.T) {
	db := memorydb.New()
	indexer, err := NewIndexedAtomicTrie(db)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		indexer.commitHeightInterval = testCommitInterval
	}

	// ensure last is uninitialised
	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	height, exists, err := indexer.Height()
	assert.EqualValues(t, 0, height)
	assert.False(t, exists)
	assert.NoError(t, err)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(0); height < 101; height++ {
		if height%2 == 0 {
			blockAtomicOpsMap[height] = nil
			continue
		}

		atomicRequests := make(map[ids.ID]*atomic.Requests)
		// only add atomic transactions to every other block
		// no atomic txs for genesis block
		for j := 0; j < rand.Intn(5); j++ {
			blockchainID := ids.GenerateTestID()
			req := atomic.Requests{
				PutRequests: []*atomic.Element{
					{
						Key:   utils.RandomBytes(16),
						Value: utils.RandomBytes(24),
						Traits: [][]byte{
							utils.RandomBytes(32),
							utils.RandomBytes(32),
						},
					},
				},
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			}
			atomicRequests[blockchainID] = &req
		}

		blockAtomicOpsMap[height] = atomicRequests
	}

	lastAcceptedBlock := newAtomicBlockFacade(100, common.Hash{}, nil)
	chainFacade := newTestChainFacade(lastAcceptedBlock, func(blockNum uint64) facades.BlockFacade {
		_, exists := blockAtomicOpsMap[blockNum]
		assert.True(t, exists)
		return newAtomicBlockFacade(blockNum, common.Hash{}, nil)
	})

	var dbCommitCount atomic2.Uint32
	doneChan := indexer.Initialize(chainFacade, func() error {
		dbCommitCount.Inc()
		return nil
	}, func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error) {
		return blockAtomicOpsMap[blk.NumberU64()], nil
	})
	<-doneChan
	assert.EqualValues(t, 1, dbCommitCount.Load())

	height, initialized, err := indexer.Height()
	assert.NoError(t, err)
	assert.True(t, initialized)
	assert.Equal(t, lastAcceptedBlock.NumberU64(), height)
}

func Test_IndexerInitializeFromState(t *testing.T) {
	db := memorydb.New()
	indexer, err := NewIndexedAtomicTrie(db)
	assert.NoError(t, err)
	assert.NotNil(t, indexer)

	{
		// for test only to make it go faaaaaasst
		atomicTrieIndexer, ok := indexer.(*indexedAtomicTrie)
		assert.True(t, ok)
		atomicTrieIndexer.commitHeightInterval = testCommitInterval
		atomicTrieIndexer.initBlockRange = 5
		atomicTrieIndexer.initialised.Store(true)
	}

	// ensure invalid height commits are not possible
	_, err = indexer.Index(999, nil)
	assert.Error(t, err)
	assert.Equal(t, 0, db.Len())

	// ensure last is uninitialised
	hash, height, err := indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), height)
	assert.Equal(t, common.Hash{}, hash)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	lastCommittedBlockHash := common.Hash{}
	// process blocks and insert them into the indexer
	for height := 0; height < 11; height++ {
		atomicRequests := make(map[ids.ID]*atomic.Requests)
		for j := 0; j < rand.Intn(5); j++ {
			blockchainID := ids.GenerateTestID()
			req := atomic.Requests{
				PutRequests: []*atomic.Element{
					{
						Key:   utils.RandomBytes(16),
						Value: utils.RandomBytes(24),
						Traits: [][]byte{
							utils.RandomBytes(32),
							utils.RandomBytes(32),
						},
					},
				},
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			}
			atomicRequests[blockchainID] = &req
		}
		hash, err := indexer.Index(uint64(height), atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			assert.NotEqual(t, common.Hash{}, hash)
			blockRootMap[uint64(height)] = hash
			lastCommittedBlockHeight = uint64(height)
			lastCommittedBlockHash = hash
		}
	}

	assert.NotEqual(t, 0, lastCommittedBlockHeight)

	hash, height, err = indexer.LastCommitted()
	assert.NoError(t, err)
	assert.Equal(t, lastCommittedBlockHeight, height)
	assert.Equal(t, lastCommittedBlockHash, hash)

	height, initialized, err := indexer.Height()
	assert.NoError(t, err)
	assert.True(t, initialized)
	assert.EqualValues(t, 10, height)

	blockAtomicOpsMap := make(map[uint64]map[ids.ID]*atomic.Requests)
	// process blocks and insert them into the indexer
	for height := uint64(11); height < 101; height++ {
		atomicRequests := make(map[ids.ID]*atomic.Requests)
		for j := 0; j < rand.Intn(5); j++ {
			blockchainID := ids.GenerateTestID()
			req := atomic.Requests{
				PutRequests: []*atomic.Element{
					{
						Key:   utils.RandomBytes(16),
						Value: utils.RandomBytes(24),
						Traits: [][]byte{
							utils.RandomBytes(32),
							utils.RandomBytes(32),
						},
					},
				},
				RemoveRequests: [][]byte{
					utils.RandomBytes(32),
					utils.RandomBytes(32),
				},
			}
			atomicRequests[blockchainID] = &req
		}
		blockAtomicOpsMap[height] = atomicRequests
	}

	lastAcceptedBlock := newAtomicBlockFacade(100, common.Hash{}, nil)
	chainFacade := newTestChainFacade(lastAcceptedBlock, func(blockNum uint64) facades.BlockFacade {
		if blockNum > lastAcceptedBlock.NumberU64() {
			return nil
		}
		// slow it down a bit
		time.Sleep(50 * time.Millisecond)
		_, exists := blockAtomicOpsMap[blockNum]
		assert.True(t, exists, "block %d must exist", blockNum)
		return newAtomicBlockFacade(blockNum, common.Hash{}, nil)
	})

	var dbCommitCount atomic2.Uint32
	doneChan := indexer.Initialize(chainFacade, func() error {
		dbCommitCount.Inc()
		return nil
	}, func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error) {
		return blockAtomicOpsMap[blk.NumberU64()], nil
	})

	// ensure calls to Index are ignored without error
	// this is important because while atomic trie indexing is running in the background
	// and as new blocks are being accepted indexer.Index will be called so it has to
	// skip this call without returning error
	time.Sleep(500 * time.Millisecond)
	hash, err = indexer.Index(lastAcceptedBlock.NumberU64()+1, nil)
	assert.NoError(t, err)
	assert.Equal(t, common.Hash{}, hash)

	// wait for index.Initialize to finish
	<-doneChan
	assert.EqualValues(t, 1, dbCommitCount.Load())

	height, initialized, err = indexer.Height()
	assert.NoError(t, err)
	assert.True(t, initialized)
	assert.EqualValues(t, lastAcceptedBlock.NumberU64(), height)
}
*/
