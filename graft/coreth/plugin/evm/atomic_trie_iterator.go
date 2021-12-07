// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/fastsync/types"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// atomicTrieIterator is an implementation of types.AtomicTrieIterator that serves
// parsed data with each iteration
type atomicTrieIterator struct {
	trieIterator *trie.Iterator   // underlying trie.Iterator
	atomicOps    *atomic.Requests // atomic operation entries at this iteration
	blockchainID ids.ID           // blockchain ID
	blockNumber  uint64           // block number at this iteration
	err          error            // error if any has occurred
}

func NewAtomicTrieIterator(trieIterator *trie.Iterator) types.AtomicTrieIterator {
	return &atomicTrieIterator{trieIterator: trieIterator}
}

// Error returns error, if any encountered during this iteration
func (a *atomicTrieIterator) Error() error {
	return a.err
}

// Next returns whether there are more nodes to iterate over
// On success, this function sets the blockNumber and atomicOps fields
// In case of an error during this iteration, it sets err and resets the above fields
func (a *atomicTrieIterator) Next() bool {
	hasNext := a.trieIterator.Next()

	err := a.trieIterator.Err
	switch {
	case err == nil && hasNext:
		// if the underlying iterator has data to iterate over, parse and set the fields
		// key is [blockNumberBytes]+[blockchainIDBytes] = 8+32=40 bytes
		keyLen := len(a.trieIterator.Key)
		expectedKeyLen := wrappers.LongLen + common.HashLength
		if keyLen != expectedKeyLen {
			// unexpected key length
			// set the error and stop the iteration as data is unreliable from this point
			a.err = fmt.Errorf("expected atomic trie key length to be %d but was %d", expectedKeyLen, keyLen)
			a.resetFields()
			return false
		}

		blockNumber := binary.BigEndian.Uint64(a.trieIterator.Key[:wrappers.LongLen])
		blockchainID, err := ids.ToID(a.trieIterator.Key[wrappers.LongLen:])
		if err != nil {
			a.err = err
			a.resetFields()
			return false
		}

		// value is RLP encoded atomic.Requests
		var requests atomic.Requests
		if err := rlp.DecodeBytes(a.trieIterator.Value, &requests); err != nil {
			a.err = err
			a.resetFields()
			return false
		}

		// success, update the struct fields
		a.blockNumber = blockNumber
		a.blockchainID = blockchainID
		a.atomicOps = &requests
	case err != nil:
		a.err = err
		fallthrough
	default:
		a.resetFields()
	}

	return hasNext
}

func (a *atomicTrieIterator) resetFields() {
	a.blockNumber = 0
	a.blockchainID = ids.ID{}
	a.atomicOps = nil
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
func (a *atomicTrieIterator) AtomicOps() *atomic.Requests {
	return a.atomicOps
}
