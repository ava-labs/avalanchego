// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpmsg

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	version = 0

	// The Results maximum size should comfortably exceed the maximum value that could happen in practice,
	// so that a correct block builder will not attempt to build a block and fail to marshal the results using the codec.
	//
	// Each Warp message costs at least 125k gas (VerifyPredicateBase since Granite; 200k before). In the worst case, each
	// message is in its own transaction and adds 61 bytes to Results: the transaction hash (32 bytes), the length of the tx
	// results (4 bytes), the precompile address (20 bytes), the length of the bitset (4 bytes), and one bitset byte (1 byte).
	//
	// A block with a 100M gas limit can verify at most 800 Warp messages, so Results is at most ~49KB, well short of the
	// 1MiB cap.
	maxResultsSize = units.MiB
)

var resultsCodec codec.Manager

func init() {
	resultsCodec = codec.NewManager(maxResultsSize)

	c := linearcodec.NewDefault()
	if err := resultsCodec.RegisterCodec(version, c); err != nil {
		panic(err)
	}
}

// BlockResults maps each transaction hash in a block to the indexes of its
// Warp messages that failed verification. A transaction without Warp messages
// has no entry.
type BlockResults map[common.Hash]set.Bits

// encodedBlockResults is the serialized form of BlockResults. Each
// transaction's bitset is keyed by [ContractAddress].
type encodedBlockResults map[common.Hash]map[common.Address][]byte

// ParseBlockResults parses bytes into BlockResults.
func ParseBlockResults(b []byte) (BlockResults, error) {
	var encodedResults encodedBlockResults
	_, err := resultsCodec.Unmarshal(b, &encodedResults)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling block results: %w", err)
	}

	results := make(BlockResults, len(encodedResults))
	for txHash, addrToBytes := range encodedResults {
		if bs, ok := addrToBytes[ContractAddress]; ok {
			results[txHash] = set.BitsFromBytes(bs)
		}
	}
	return results, nil
}

// Get returns the results for txHash.
func (b BlockResults) Get(txHash common.Hash) set.Bits {
	if result, ok := b[txHash]; ok {
		return result
	}
	return set.NewBits()
}

// Set sets the results for txHash, overwriting any previous results.
func (b *BlockResults) Set(txHash common.Hash, results set.Bits) {
	if *b == nil {
		*b = make(BlockResults)
	}
	(*b)[txHash] = results
}

// Bytes marshals the results.
func (b BlockResults) Bytes() ([]byte, error) {
	// Convert to the encoded representation before marshaling to avoid
	// serializing set.Bits directly, which is not supported by the codec.
	results := make(encodedBlockResults, len(b))
	for txHash, bits := range b {
		results[txHash] = map[common.Address][]byte{
			ContractAddress: bits.Bytes(),
		}
	}
	return resultsCodec.Marshal(version, results)
}
