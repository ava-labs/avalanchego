package evm

import (
	"bytes"
	"encoding/binary"
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
	acceptedHeightAtomicTxDB database.Database
	codec                    codec.Manager
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore, acceptedHeightAtomicTxDB database.Database, codec codec.Manager) (types.AtomicTrie, error) {
	iTrie, err := NewIndexedAtomicTrie(db)
	if err != nil {
		return nil, err
	}
	return &blockingAtomicTrie{
		indexedAtomicTrie:        iTrie.(*indexedAtomicTrie),
		acceptedHeightAtomicTxDB: acceptedHeightAtomicTxDB,
		codec:                    codec,
	}, nil
}

func (b *blockingAtomicTrie) Initialize(chain facades.ChainFacade, dbCommitFn func() error, getAtomicTxFn func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error)) <-chan error {
	resultChan := make(chan error, 1)

	go func(chain facades.ChainFacade, dbCommitFn func() error, resultChan chan<- error) {
		defer close(resultChan)
		lastAccepted := chain.LastAcceptedBlock()
		iter := b.acceptedHeightAtomicTxDB.NewIterator()
		transactionsIndexed := uint64(0)
		startTime := time.Now()
		lastUpdate := time.Now()
		for iter.Next() && iter.Error() == nil {
			heightBytes := iter.Key()
			if len(heightBytes) != wrappers.LongLen ||
				bytes.Equal(heightBytes, heightAtomicTxDBInitializedKey) {
				// this is metadata key, skip it
				continue
			}

			height := binary.BigEndian.Uint64(heightBytes)
			if height > lastAccepted.NumberU64() {
				// skip tx if height is > last accepted
				continue
			}

			txBytes := iter.Value()

			tx := &Tx{}
			if _, err := b.codec.Unmarshal(txBytes, tx); err != nil {
				log.Error("problem parsing atomic transaction from db", "err", err)
				resultChan <- err
				return
			}
			if err := tx.Sign(b.codec, nil); err != nil {
				log.Error("problem initializing atomic transaction from DB", "err", err)
				resultChan <- err
				return
			}
			ops, err := tx.AtomicOps()
			if err != nil {
				log.Error("problem getting atomic ops", "err", err)
				resultChan <- err
				return
			}

			binary.BigEndian.PutUint64(heightBytes, height)
			err = b.updateTrie(ops, heightBytes)
			if err != nil {
				log.Error("problem indexing atomic ops", "err", err)
				resultChan <- err
				return
			}

			transactionsIndexed++
			if time.Since(lastUpdate) > 30*time.Second {
				log.Info("atomic trie init progress", "indexedTransactions", transactionsIndexed)
				lastUpdate = time.Now()
			}
		}

		if iter.Error() != nil {
			log.Error("error iterating data", "err", iter.Error())
			resultChan <- iter.Error()
			return
		}

		log.Info("done updating trie, setting index height", "height", lastAccepted.NumberU64(), "duration", time.Since(startTime))
		if err := b.setIndexHeight(lastAccepted.NumberU64()); err != nil {
			log.Error("error setting index height", "height", lastAccepted.NumberU64())
			resultChan <- err
			return
		}

		log.Info("committing trie")
		hash, err := b.commitTrie()
		if err != nil {
			log.Error("error committing trie post init")
			resultChan <- err
			return
		}

		log.Info("trie committed", "hash", hash, "height", lastAccepted.NumberU64(), "time", time.Since(startTime))

		if dbCommitFn != nil {
			log.Info("committing DB")
			if err := dbCommitFn(); err != nil {
				log.Error("unable to commit DB")
				resultChan <- err
				return
			}
		}

		defer log.Info("atomic trie initialisation complete", "time", time.Since(startTime))

		b.indexedAtomicTrie.initialised.Store(true)
	}(chain, dbCommitFn, resultChan)

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
