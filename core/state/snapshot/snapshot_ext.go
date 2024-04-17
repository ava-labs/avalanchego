package snapshot

import (
	"time"

	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
)

func (t *Tree) DiskAccountIterator(seek common.Hash) AccountIterator {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.disklayer().AccountIterator(seek)
}

func (t *Tree) DiskStorageIterator(account common.Hash, seek common.Hash) StorageIterator {
	t.lock.Lock()
	defer t.lock.Unlock()

	it, _ := t.disklayer().StorageIterator(account, seek)
	return it
}

// NewDiskLayer creates a diskLayer for direct access to the contents of the on-disk
// snapshot. Does not perform any validation.
func NewDiskLayer(diskdb ethdb.KeyValueStore) Snapshot {
	return &diskLayer{
		diskdb:  diskdb,
		created: time.Now(),

		// state sync uses iterators to access data, so this cache is not used.
		// initializing it out of caution.
		cache: utils.NewMeteredCache(32*1024, "", 0),
	}
}

// NewTestTree creates a *Tree with a pre-populated diskLayer
func NewTestTree(diskdb ethdb.KeyValueStore, blockHash, root common.Hash) *Tree {
	base := &diskLayer{
		diskdb:    diskdb,
		root:      root,
		blockHash: blockHash,
		cache:     utils.NewMeteredCache(128*256, "", 0),
		created:   time.Now(),
	}
	return &Tree{
		blockLayers: map[common.Hash]snapshot{
			blockHash: base,
		},
		stateLayers: map[common.Hash]map[common.Hash]snapshot{
			root: {
				blockHash: base,
			},
		},
	}
}
