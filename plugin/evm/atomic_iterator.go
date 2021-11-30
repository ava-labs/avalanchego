// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/rlp"
)

// atomicTrieIterator is an implementation of types.AtomicTrieIterator that serves
// parsed data with each iteration
type atomicTrieIterator struct {
	trieIterator *trie.Iterator              // underlying trie.Iterator
	atomicOps    map[ids.ID]*atomic.Requests // atomic operation entries at this iteration
	blockNumber  uint64                      // block number at this iteration
	err          error                       // error if any has occurred
}

func NewAtomicTrieIterator(trieIterator *trie.Iterator) types.AtomicTrieIterator {
	return &atomicTrieIterator{trieIterator: trieIterator}
}

// Error returns error, if any encountered during this iteration
func (a *atomicTrieIterator) Error() error {
	return a.err
}

// Next returns whether there is data to iterate over
// this function sets blockNumber, blockchainID and entries fields of the atomicTrieIterator
// resets all fields and sets err in case of an error during current iteration
func (a *atomicTrieIterator) Next() bool {
	hasNext := a.trieIterator.Next()
	// if the underlying iterator has data to iterate over, parse and set the fields
	if err := a.trieIterator.Err; err == nil && hasNext {
		// key is [blockNumberBytes]+[blockchainIDBytes]
		blockNumber := binary.BigEndian.Uint64(a.trieIterator.Key[:wrappers.LongLen])
		blockchainID, err := ids.ToID(a.trieIterator.Key[wrappers.LongLen:])
		if err != nil {
			a.err = err
			a.resetFields()
			return false
		}

		// value is RLP encoded atomic.Requests
		var requests atomic.Requests
		if err = rlp.DecodeBytes(a.trieIterator.Value, &requests); err != nil {
			a.err = err
			a.resetFields()
			return false
		}

		// update the struct fields
		a.blockNumber = blockNumber
		a.atomicOps = map[ids.ID]*atomic.Requests{blockchainID: &requests}
	} else if err != nil {
		a.err = err
		a.resetFields()
	} else {
		a.resetFields()
	}
	return hasNext
}

func (a *atomicTrieIterator) resetFields() {
	a.blockNumber = 0
	a.atomicOps = nil
}

// BlockNumber returns the block number of this iteration
func (a *atomicTrieIterator) BlockNumber() uint64 {
	return a.blockNumber
}

func (a *atomicTrieIterator) AtomicOps() map[ids.ID]*atomic.Requests {
	return a.atomicOps
}
