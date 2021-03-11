// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkeddb

import (
	"sync"

	"github.com/ava-labs/avalanchego/database"
)

var (
	headKey = []byte{0x01}
)

// LinkedDB provides a key value interface while allowing iteration.
type LinkedDB interface {
	database.KeyValueReader
	database.KeyValueWriter

	NewIterator() database.Iterator
}

type linkedDB struct {
	lock                                                                   sync.RWMutex
	headKeyLock                                                            sync.Mutex
	headKeyIsSynced, headKeyExists, headKeyIsUpdated, updatedHeadKeyExists bool
	headKey, updatedHeadKey                                                []byte
	db                                                                     database.Database
	batch                                                                  database.Batch
}

type node struct {
	Value       []byte `serialize:"true"`
	HasNext     bool   `serialize:"true"`
	Next        []byte `serialize:"true"`
	HasPrevious bool   `serialize:"true"`
	Previous    []byte `serialize:"true"`
}

func New(db database.Database) LinkedDB {
	return &linkedDB{
		db:    db,
		batch: db.NewBatch(),
	}
}

func (ldb *linkedDB) Has(key []byte) (bool, error) {
	ldb.lock.RLock()
	defer ldb.lock.RUnlock()

	return ldb.db.Has(nodeKey(key))
}

func (ldb *linkedDB) Get(key []byte) ([]byte, error) {
	ldb.lock.RLock()
	defer ldb.lock.RUnlock()

	node, err := ldb.getNode(key)
	return node.Value, err
}

func (ldb *linkedDB) Put(key, value []byte) error {
	ldb.lock.Lock()
	defer ldb.lock.Unlock()

	ldb.resetBatch()

	// If the key already has a node in the list, update that node.
	existingNode, err := ldb.getNode(key)
	if err == nil {
		existingNode.Value = value
		if err := ldb.putNode(key, existingNode); err != nil {
			return err
		}
		return ldb.writeBatch()
	}
	if err != database.ErrNotFound {
		return err
	}

	// The key isn't currently in the list, so we should add it as the head.
	newHead := node{
		Value: value,
	}
	if headKey, err := ldb.getHeadKey(); err == nil {
		// The list currently has a head, so we need to update the old head.
		oldHead, err := ldb.getNode(headKey)
		if err != nil {
			return err
		}
		oldHead.HasPrevious = true
		oldHead.Previous = key
		if err := ldb.putNode(headKey, oldHead); err != nil {
			return err
		}

		newHead.HasNext = true
		newHead.Next = headKey
	} else if err != database.ErrNotFound {
		return err
	}
	if err := ldb.putNode(key, newHead); err != nil {
		return err
	}
	if err := ldb.putHeadKey(key); err != nil {
		return err
	}
	return ldb.writeBatch()
}

func (ldb *linkedDB) Delete(key []byte) error {
	ldb.lock.Lock()
	defer ldb.lock.Unlock()

	currentNode, err := ldb.getNode(key)
	if err == database.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	ldb.resetBatch()

	// We're trying to delete this node.
	if err := ldb.deleteNode(key); err != nil {
		return err
	}

	if !currentNode.HasPrevious {
		// This node was the head, so we must change the head.
		if !currentNode.HasNext {
			// This is the only node, so we can just delete it.
			if err := ldb.deleteHeadKey(); err != nil {
				return err
			}
			return ldb.writeBatch()
		}

		// The next node will be the new head.
		if err := ldb.putHeadKey(currentNode.Next); err != nil {
			return err
		}

		// The next node is going to be the new head, so we must configure that
		// node.
		newHead, err := ldb.getNode(currentNode.Next)
		if err != nil {
			return err
		}
		newHead.HasPrevious = false
		newHead.Previous = nil

		if err := ldb.putNode(currentNode.Next, newHead); err != nil {
			return err
		}

		return ldb.writeBatch()
	}

	// The node we are deleting isn't the head node, so we must update the
	// previous node.

	previousNode, err := ldb.getNode(currentNode.Previous)
	if err != nil {
		return err
	}
	previousNode.HasNext = currentNode.HasNext
	previousNode.Next = currentNode.Next
	if err := ldb.putNode(currentNode.Previous, previousNode); err != nil {
		return err
	}

	if currentNode.HasNext {
		// If the node we are deleting isn't the tail, we need to notify the
		// next node who it should now point to.
		nextNode, err := ldb.getNode(currentNode.Next)
		if err != nil {
			return err
		}
		nextNode.HasPrevious = true
		nextNode.Previous = currentNode.Previous
		if err := ldb.putNode(currentNode.Next, nextNode); err != nil {
			return err
		}
	}
	return ldb.writeBatch()
}

func (ldb *linkedDB) NewIterator() database.Iterator { return &iterator{ldb: ldb} }

func (ldb *linkedDB) getHeadKey() ([]byte, error) {
	// If the ldb read lock is held, then there needs to be additional
	// synchronization here to avoid racy behavior.
	ldb.headKeyLock.Lock()
	defer ldb.headKeyLock.Unlock()

	if ldb.headKeyIsSynced {
		if ldb.headKeyExists {
			return ldb.headKey, nil
		}
		return nil, database.ErrNotFound
	}
	headKey, err := ldb.db.Get(headKey)
	if err == nil {
		ldb.headKeyIsSynced = true
		ldb.headKeyExists = true
		ldb.headKey = headKey
		return headKey, nil
	}
	if err == database.ErrNotFound {
		ldb.headKeyIsSynced = true
		ldb.headKeyExists = false
		return nil, database.ErrNotFound
	}
	return headKey, err
}

func (ldb *linkedDB) putHeadKey(key []byte) error {
	ldb.headKeyIsUpdated = true
	ldb.updatedHeadKeyExists = true
	ldb.updatedHeadKey = key
	return ldb.batch.Put(headKey, key)
}

func (ldb *linkedDB) deleteHeadKey() error {
	ldb.headKeyIsUpdated = true
	ldb.updatedHeadKeyExists = false
	return ldb.batch.Delete(headKey)
}

func (ldb *linkedDB) getNode(key []byte) (node, error) {
	nodeBytes, err := ldb.db.Get(nodeKey(key))
	if err != nil {
		return node{}, err
	}
	n := node{}
	_, err = c.Unmarshal(nodeBytes, &n)
	return n, err
}

func (ldb *linkedDB) putNode(key []byte, n node) error {
	nodeBytes, err := c.Marshal(codecVersion, n)
	if err != nil {
		return err
	}
	return ldb.batch.Put(nodeKey(key), nodeBytes)
}

func (ldb *linkedDB) deleteNode(key []byte) error { return ldb.batch.Delete(nodeKey(key)) }

func (ldb *linkedDB) resetBatch() {
	ldb.headKeyIsUpdated = false
	ldb.batch.Reset()
}
func (ldb *linkedDB) writeBatch() error {
	if err := ldb.batch.Write(); err != nil || !ldb.headKeyIsUpdated {
		return err
	}
	ldb.headKeyIsSynced = true
	ldb.headKeyExists = ldb.updatedHeadKeyExists
	ldb.headKey = ldb.updatedHeadKey
	return nil
}

type iterator struct {
	ldb                    *linkedDB
	initialized, exhausted bool
	key, value, nextKey    []byte
	err                    error
}

// Next implements the Iterator interface
func (it *iterator) Next() bool {
	// If the iterator has been exhausted, there is no next value.
	if it.exhausted {
		it.key = nil
		it.value = nil
		return true
	}

	it.ldb.lock.RLock()
	defer it.ldb.lock.RUnlock()

	// If the iterator was not yet initialized, do it now.
	if !it.initialized {
		it.initialized = true
		headKey, err := it.ldb.getHeadKey()
		if err == database.ErrNotFound {
			it.exhausted = true
			it.key = nil
			it.value = nil
			return true
		}
		if err != nil {
			it.exhausted = true
			it.key = nil
			it.value = nil
			it.err = err
			return true
		}
		it.nextKey = headKey
	}

	nextNode, err := it.ldb.getNode(it.nextKey)
	if err != nil {
		it.exhausted = true
		it.key = nil
		it.value = nil
		it.err = err
		return true
	}
	it.key = it.nextKey
	it.value = nextNode.Value
	it.nextKey = nextNode.Next
	it.exhausted = !nextNode.HasNext
	return false
}

// Error implements the Iterator interface
func (it *iterator) Error() error { return it.err }

// Key implements the Iterator interface
func (it *iterator) Key() []byte { return it.key }

// Value implements the Iterator interface
func (it *iterator) Value() []byte { return it.value }

// Release implements the Iterator interface
func (it *iterator) Release() {}

func nodeKey(key []byte) []byte {
	newKey := make([]byte, len(key)+1)
	copy(newKey[1:], key)
	return newKey
}
