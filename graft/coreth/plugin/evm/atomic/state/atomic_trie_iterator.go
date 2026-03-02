// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var errKeyLength = errors.New("atomic trie key length invalid")

type atomicTrieIterator struct {
	trieIterator *trie.Iterator // underlying trie.Iterator
	codec        codec.Manager
	key          []byte
	atomicOps    *atomic.Requests // atomic operation entries at this iteration
	blockchainID ids.ID           // blockchain ID
	blockNumber  uint64           // block number at this iteration
	err          error            // error if any has occurred
}

func NewAtomicTrieIterator(trieIterator *trie.Iterator, codec codec.Manager) *atomicTrieIterator {
	return &atomicTrieIterator{trieIterator: trieIterator, codec: codec}
}

// Error returns error, if any encountered during this iteration
func (a *atomicTrieIterator) Error() error {
	return a.err
}

// Next returns whether there are more nodes to iterate over
// On success, this function sets the blockNumber and atomicOps fields
// In case of an error during this iteration, it sets the error value and resets the above fields.
// It is the responsibility of the caller to check the result of Error() after an iterator reports
// having no more elements to iterate.
func (a *atomicTrieIterator) Next() bool {
	if !a.trieIterator.Next() {
		a.resetFields(a.trieIterator.Err)
		return false
	}

	// if the underlying iterator has data to iterate over, parse and set the fields
	// key is [blockNumberBytes]+[blockchainIDBytes] = 8+32=40 bytes
	keyLen := len(a.trieIterator.Key)
	// If the key has an unexpected length, set the error and stop the iteration since the data is
	// no longer reliable.
	if keyLen != TrieKeyLength {
		a.resetFields(fmt.Errorf("%w: expected %d but was %d", errKeyLength, TrieKeyLength, keyLen))
		return false
	}

	blockNumber := binary.BigEndian.Uint64(a.trieIterator.Key[:wrappers.LongLen])
	blockchainID, err := ids.ToID(a.trieIterator.Key[wrappers.LongLen:])
	if err != nil {
		a.resetFields(err)
		return false
	}

	// The value in the iterator should be the atomic requests serialized the the codec.
	requests := new(atomic.Requests)
	if _, err = a.codec.Unmarshal(a.trieIterator.Value, requests); err != nil {
		a.resetFields(err)
		return false
	}

	// Success, update the struct fields
	a.blockNumber = blockNumber
	a.blockchainID = blockchainID
	a.atomicOps = requests
	a.key = a.trieIterator.Key // trieIterator.Key is already newly allocated so copy is not needed here
	return true
}

// resetFields resets the value fields of the iterator to their nil values and sets the error value to [err].
func (a *atomicTrieIterator) resetFields(err error) {
	a.err = err
	a.blockNumber = 0
	a.blockchainID = ids.ID{}
	a.atomicOps = nil
	a.key = nil
}

// BlockNumber returns the current block number
func (a *atomicTrieIterator) BlockNumber() uint64 {
	return a.blockNumber
}

// BlockchainID returns the current blockchain ID at the current block number
func (a *atomicTrieIterator) BlockchainID() ids.ID {
	return a.blockchainID
}

// AtomicOps returns atomic requests for the blockchainID at the current block number
// returned object can be freely modified
func (a *atomicTrieIterator) AtomicOps() *atomic.Requests {
	return a.atomicOps
}

// Key returns the current database key that the iterator is iterating
// returned []byte can be freely modified
func (a *atomicTrieIterator) Key() []byte {
	return a.key
}

// Value returns the current database value that the iterator is iterating
func (a *atomicTrieIterator) Value() []byte {
	return a.trieIterator.Value
}
