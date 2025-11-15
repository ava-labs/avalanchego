// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/coreth/utils"
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

type SnapshotIterable interface {
	Snapshot

	// AccountIterator creates an account iterator over an arbitrary layer.
	AccountIterator(seek common.Hash) AccountIterator

	// StorageIterator creates a storage iterator over an arbitrary layer.
	StorageIterator(account common.Hash, seek common.Hash) (StorageIterator, bool)
}

// NewDiskLayer creates a diskLayer for direct access to the contents of the on-disk
// snapshot. Does not perform any validation.
func NewDiskLayer(diskdb ethdb.KeyValueStore) SnapshotIterable {
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
