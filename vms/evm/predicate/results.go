// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

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
	// so that a correct block builder will not attempt to build a block and fail to marshal the predicate results using the codec.
	//
	// We make this easy to reason about by assigning a minimum gas cost to the `PredicateGas` function of precompiles.
	// In the case of Warp, the minimum gas cost is set to 200k gas, which can lead to at most 32 additional bytes being included in Results.
	//
	// The additional bytes come from the transaction hash (32 bytes), length of tx predicate results (4 bytes),
	// the precompile address (20 bytes), length of the bytes result (4 bytes), and the additional byte in the results bitset (1 byte).
	//
	// This results in 200k gas contributing a maximum of 61 additional bytes to Result.
	// For a block with a maximum gas limit of 100M, the block can include up to 500 validated predicates based contributing to the size of Result.
	//
	// At 61 bytes / validated predicate, this yields ~30KB, which is well short of the 1MB cap.
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

type (
	// PrecompileResults is a map of results for each precompile address to the
	// resulting bitset.
	PrecompileResults map[common.Address]set.Bits

	// BlockResults maps the transactions in a block to their precompile
	// results.
	BlockResults map[common.Hash]PrecompileResults

	// encodedBlockResults is used to serialize and deserialize BlockResults.
	encodedBlockResults map[common.Hash]map[common.Address][]byte
)

// ParseBlockResults parses bytes into predicate results.
func ParseBlockResults(b []byte) (BlockResults, error) {
	var encodedResults encodedBlockResults
	_, err := resultsCodec.Unmarshal(b, &encodedResults)
	if err != nil {
		return BlockResults{}, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}

	// Convert encoded representation into in-memory representation
	results := make(BlockResults, len(encodedResults))
	for txHash, addrToBytes := range encodedResults {
		decoded := make(PrecompileResults, len(addrToBytes))
		for addr, bs := range addrToBytes {
			decoded[addr] = set.BitsFromBytes(bs)
		}
		results[txHash] = decoded
	}

	return results, nil
}

// Get returns the predicate results for txHash from precompile address.
func (b *BlockResults) Get(txHash common.Hash, address common.Address) set.Bits {
	if result, ok := (*b)[txHash][address]; ok {
		return result
	}
	return set.NewBits()
}

// Set sets the predicate results for the given txHash. Results are overwritten,
// not merged.
func (b *BlockResults) Set(txHash common.Hash, txResults PrecompileResults) {
	if len(txResults) == 0 {
		delete(*b, txHash)
		return
	}

	if *b == nil {
		*b = make(map[common.Hash]PrecompileResults)
	}
	(*b)[txHash] = txResults
}

// Bytes marshals the predicate results.
func (b *BlockResults) Bytes() ([]byte, error) {
	// Convert to results representation before marshaling to avoid serializing
	// set.Bits directly, which is not supported by the codec.
	results := make(encodedBlockResults, len(*b))
	for txHash, addrToBits := range *b {
		encoded := make(map[common.Address][]byte, len(addrToBits))
		for addr, bits := range addrToBits {
			encoded[addr] = bits.Bytes()
		}
		results[txHash] = encoded
	}

	return resultsCodec.Marshal(version, results)
}
