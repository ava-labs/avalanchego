package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/fastsync/facades"
	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type blockingAtomicTrie struct {
	*indexedAtomicTrie
	acceptedAtomicTxDB         database.Database
	codec                      codec.Manager
	prefix                     []byte
	parseAcceptedAtomicTxBytes func(bytes []byte) (*Tx, uint64, error)
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore, acceptedAtomicTxDB database.Database, codec codec.Manager, prefix []byte, parseAcceptedAtomicTxBytes func(bytes []byte) (*Tx, uint64, error)) (types.AtomicTrie, error) {
	iTrie, err := NewIndexedAtomicTrie(db)
	if err != nil {
		return nil, err
	}
	return &blockingAtomicTrie{
		indexedAtomicTrie:          iTrie.(*indexedAtomicTrie),
		acceptedAtomicTxDB:         acceptedAtomicTxDB,
		codec:                      codec,
		prefix:                     prefix,
		parseAcceptedAtomicTxBytes: parseAcceptedAtomicTxBytes,
	}, nil
}

func (b *blockingAtomicTrie) Initialize(chain facades.ChainFacade, dbCommitFn func() error, getAtomicTxFn func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error)) chan error {
	lastAcceptedBlockHeight := chain.LastAcceptedBlock().NumberU64()
	resultChan := make(chan error)
	init := func() error {
		_, lastIndexedHeight, err := b.indexedAtomicTrie.LastCommitted()
		if err != nil {
			return err
		}

		startBytes := make([]byte, len(b.prefix)+wrappers.LongLen)
		packer := wrappers.Packer{Bytes: startBytes}
		packer.PackFixedBytes(b.prefix)
		packer.PackLong(uint64(lastIndexedHeight))

		toIdx := lastIndexedHeight

		it := b.acceptedAtomicTxDB.NewIteratorWithStartAndPrefix(startBytes, b.prefix)
		for it.Next() {
			if err := it.Error(); err != nil {
				return err
			}
			txIdLength := len(ids.ID{})
			if len(it.Key()) != wrappers.LongLen+txIdLength {
				continue
			}
			packer := wrappers.Packer{Bytes: it.Key()}
			expectedEqualToPrefix := packer.UnpackFixedBytes(len(b.prefix))
			if string(expectedEqualToPrefix) != string(b.prefix) {
				return fmt.Errorf("expected prefix (%s) got (%s)", string(b.prefix), string(expectedEqualToPrefix))
			}
			height := packer.UnpackLong()
			txID, err := ids.ToID(packer.UnpackFixedBytes(txIdLength))
			if err != nil {
				return err
			}
			tx, parsedHeight, err := b.parseAcceptedAtomicTxBytes(it.Value())
			if tx.ID() != txID {
				return fmt.Errorf("mismatch in tx.ID (%v) with txID (%v)", tx.ID(), txID)
			}
			if height != parsedHeight {
				return fmt.Errorf("mismatch in height (%v) with parsedHeight(%v)", height, parsedHeight)
			}

			// nil idx all missing ones
			for ; toIdx < height; toIdx++ {
				if _, err := b.indexedAtomicTrie.Index(toIdx, nil); err != nil {
					return err
				}
			}

			atomicOps, err := tx.AtomicOps()
			if err != nil {
				return err
			}
			if _, err := b.indexedAtomicTrie.Index(height, atomicOps); err != nil {
				return err
			}

			toIdx = height + 1
		}

		for ; toIdx <= lastAcceptedBlockHeight; toIdx++ {
			if _, err := b.indexedAtomicTrie.Index(toIdx, nil); err != nil {
				return err
			}
		}
		return nil
	}
	go func() {
		resultChan <- init()
	}()

	startTime := time.Now()
	/////////////////////
	log.Info("done updating trie, setting index height", "height", lastAcceptedBlockHeight, "duration", time.Since(startTime))
	if err := b.setIndexHeight(lastAcceptedBlockHeight); err != nil {
		log.Crit("error setting index height", "height", lastAcceptedBlockHeight)
		return nil
	}

	log.Info("committing trie")
	hash, err := b.commitTrie()
	if err != nil {
		log.Crit("error committing trie post init")
		return nil
	}

	log.Info("trie committed", "hash", hash, "height", lastAcceptedBlockHeight, "time", time.Since(startTime))

	if dbCommitFn != nil {
		log.Info("committing DB")
		if err := dbCommitFn(); err != nil {
			log.Crit("unable to commit DB")
			return nil
		}
	}

	defer log.Info("atomic trie initialisation complete", "time", time.Since(startTime))
	return resultChan
}

func (b *blockingAtomicTrie) Index(height uint64, atomicOps map[ids.ID]*atomic.Requests) (common.Hash, error) {
	return b.indexedAtomicTrie.Index(height, atomicOps)
}

func (b *blockingAtomicTrie) Iterator(hash common.Hash) (types.AtomicIterator, error) {
	return b.indexedAtomicTrie.Iterator(hash)
}

func (b *blockingAtomicTrie) Height() (uint64, bool, error) {
	return b.indexedAtomicTrie.Height()
}

func (b *blockingAtomicTrie) LastCommitted() (common.Hash, uint64, error) {
	return b.indexedAtomicTrie.LastCommitted()
}
