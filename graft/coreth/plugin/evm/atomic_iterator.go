package evm

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/log"

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
	errs         []error                     // errors if any so far
}

func NewAtomicTrieIterator(trieIterator *trie.Iterator) types.AtomicTrieIterator {
	return &atomicTrieIterator{trieIterator: trieIterator}
}

// Errors returns a list of errors, if any encountered so far
func (a *atomicTrieIterator) Errors() []error {
	if a.trieIterator.Err == nil && len(a.errs) == 0 {
		return nil
	}

	// check if there's an error in the trie iterator
	// if there is one, we include this in the []error len
	trieErrLen := 0
	if a.trieIterator.Err != nil {
		trieErrLen = 1
	}

	// create a slice to copy errors into and return the copy
	errsCopy := make([]error, len(a.errs), len(a.errs)+trieErrLen)
	copy(errsCopy, a.errs)
	errsCopy = append(errsCopy, a.trieIterator.Err)
	return errsCopy
}

// Next returns whether there is data to iterate over
// this function sets blockNumber, blockchainID and entries fields of the atomicTrieIterator
// resets all fields and sets err in case of an error during current iteration
func (a *atomicTrieIterator) Next() bool {
	hasNext := a.trieIterator.Next()
	// if the underlying iterator has data to iterate over, parse and set the fields
	if hasNext {
		// key is [blockNumberBytes]+[blockchainIDBytes]
		blockNumber := binary.BigEndian.Uint64(a.trieIterator.Key[:wrappers.LongLen])
		blockchainID, err := ids.ToID(a.trieIterator.Key[wrappers.LongLen:])
		if err != nil {
			a.errs = append(a.errs, err)
			log.Error("error converting to ID", "err", err)
			a.resetFields()
			return hasNext
		}

		// value is RLP encoded atomic.Requests
		var requests atomic.Requests
		if err = rlp.DecodeBytes(a.trieIterator.Value, &requests); err != nil {
			a.errs = append(a.errs, err)
			log.Error("error decoding", "err", err)
			a.resetFields()
			return hasNext
		}

		// update the struct fields
		a.blockNumber = blockNumber
		a.atomicOps = map[ids.ID]*atomic.Requests{blockchainID: &requests}
	} else {
		// else reset the fields
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
