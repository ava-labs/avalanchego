package evm

import (
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
	acceptedAtomicTxDB database.Database
	codec              codec.Manager
}

func NewBlockingAtomicTrie(db ethdb.KeyValueStore, acceptedAtomicTxDB database.Database, codec codec.Manager) (types.AtomicTrie, error) {
	iTrie, err := NewIndexedAtomicTrie(db)
	if err != nil {
		return nil, err
	}
	return &blockingAtomicTrie{
		indexedAtomicTrie:  iTrie.(*indexedAtomicTrie),
		acceptedAtomicTxDB: acceptedAtomicTxDB,
		codec:              codec,
	}, nil
}

func (b *blockingAtomicTrie) Initialize(chain facades.ChainFacade, dbCommitFn func() error, getAtomicTxFn func(blk facades.BlockFacade) (map[ids.ID]*atomic.Requests, error)) chan struct{} {
	lastAccepted := chain.LastAcceptedBlock()
	iter := b.acceptedAtomicTxDB.NewIterator()
	for iter.Next() && iter.Error() == nil {
		indexedTxBytes := iter.Value()
		packer := wrappers.Packer{Bytes: indexedTxBytes}
		height := packer.UnpackLong()
		if height > lastAccepted.NumberU64() {
			// skip tx if height is > last accepted
			continue
		}

		txBytes := packer.UnpackBytes()

		tx := &Tx{}
		if _, err := b.codec.Unmarshal(txBytes, tx); err != nil {
			log.Crit("problem parsing atomic transaction from db", "err", err)
			return nil
		}
		if err := tx.Sign(b.codec, nil); err != nil {
			log.Crit("problem initializing atomic transaction from DB", "err", err)
			return nil
		}
		ops, err := tx.AtomicOps()
		if err != nil {
			log.Crit("problem getting atomic ops", "err", err)
			return nil
		}
		hash, err := b.index(height, ops)
		if err != nil {
			log.Crit("problem indexing atomic ops", "err", err)
			return nil
		}
		if hash != (common.Hash{}) {
			log.Info("committed trie", "height", height, "hash", hash)
			if err := dbCommitFn(); err != nil {
				log.Crit("error committing DB", "err", err)
				return nil
			}
		}
	}
	doneChan := make(chan struct{}, 1)
	b.indexedAtomicTrie.initialised.Store(true)
	defer close(doneChan)
	return doneChan
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
