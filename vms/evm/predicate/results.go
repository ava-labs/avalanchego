// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	version        = 0
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
