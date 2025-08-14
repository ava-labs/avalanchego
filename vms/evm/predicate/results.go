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

// PrecompileResults is a map of results for each precompile address to the resulting bitset.
type PrecompileResults map[common.Address]set.Bits

// BlockResults encodes the precompile predicate results included in a block on a per transaction basis.
// BlockResults is not thread-safe.
type BlockResults struct {
	TxResults map[common.Hash]PrecompileResults `serialize:"true"`
}

// ParseBlockResults parses bytes into predicate results.
func ParseBlockResults(b []byte) (BlockResults, error) {
	var encodedResults encodedBlockResults
	_, err := resultsCodec.Unmarshal(b, &encodedResults)
	if err != nil {
		return BlockResults{}, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}

	// Convert encoded representation into in-memory representation
	out := BlockResults{TxResults: make(map[common.Hash]PrecompileResults, len(encodedResults.TxResults))}
	for txHash, addrToBytes := range encodedResults.TxResults {
		decoded := make(PrecompileResults, len(addrToBytes))
		for addr, bs := range addrToBytes {
			decoded[addr] = set.BitsFromBytes(bs)
		}
		out.TxResults[txHash] = decoded
	}

	return out, nil
}

// Get returns the predicate results for txHash from precompile address if available.
// Returns (set.Bits{}, false) if the txHash or address is not found.
func (b *BlockResults) Get(txHash common.Hash, address common.Address) (set.Bits, bool) {
	result, ok := b.TxResults[txHash][address]
	return result, ok
}

// Set sets the predicate results for the given txHash. Overrides results if present.
func (b *BlockResults) Set(txHash common.Hash, txResults PrecompileResults) {
	if b.TxResults == nil {
		b.TxResults = make(map[common.Hash]PrecompileResults)
	}
	b.TxResults[txHash] = txResults
}

// Bytes marshals the current state of predicate results
func (b *BlockResults) Bytes() ([]byte, error) {
	// Convert to encoded representation before marshaling to avoid serializing
	// set.Bits directly, which is not supported by the reflect-based codec.
	encoded := encodedBlockResults{TxResults: make(map[common.Hash]map[common.Address][]byte, len(b.TxResults))}
	for txHash, addrToBits := range b.TxResults {
		enc := make(map[common.Address][]byte, len(addrToBits))
		for addr, bits := range addrToBits {
			enc[addr] = bits.Bytes()
		}
		encoded.TxResults[txHash] = enc
	}

	return resultsCodec.Marshal(version, &encoded)
}

// encodedBlockResults is the serialized representation of BlockResults.
type encodedBlockResults struct {
	TxResults map[common.Hash]map[common.Address][]byte `serialize:"true"`
}
