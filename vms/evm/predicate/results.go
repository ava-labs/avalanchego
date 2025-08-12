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

// encodedPrecompileResults is the on-wire representation of PrecompileResults,
// which encodes the bitset as compact bytes.
type encodedPrecompileResults map[common.Address][]byte

// BlockResults encodes the precompile predicate results included in a block on a per transaction basis.
// BlockResults is not thread-safe.
type BlockResults struct {
	TxResults map[common.Hash]PrecompileResults `serialize:"true"`
}

// encodedBlockResults is the on-wire representation of BlockResults.
type encodedBlockResults struct {
	TxResults map[common.Hash]encodedPrecompileResults `serialize:"true"`
}

// NewBlockResults returns an empty predicate results.
func NewBlockResults() BlockResults {
	return BlockResults{
		TxResults: make(map[common.Hash]PrecompileResults),
	}
}

// ParseBlockResults parses bytes into predicate results.
func ParseBlockResults(b []byte) (BlockResults, error) {
	wire := new(encodedBlockResults)
	_, err := resultsCodec.Unmarshal(b, wire)
	if err != nil {
		return BlockResults{}, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}

	// Convert wire representation into in-memory representation
	out := BlockResults{TxResults: make(map[common.Hash]PrecompileResults, len(wire.TxResults))}
	for txHash, addrToBytes := range wire.TxResults {
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
func (r *BlockResults) Get(txHash common.Hash, address common.Address) (set.Bits, bool) {
	result, ok := r.TxResults[txHash][address]
	return result, ok
}

// Set sets the predicate results for the given txHash. Overrides results if present.
func (r *BlockResults) Set(txHash common.Hash, txResults PrecompileResults) {
	if r.TxResults == nil {
		r.TxResults = make(map[common.Hash]PrecompileResults)
	}
	r.TxResults[txHash] = txResults
}

// Bytes marshals the current state of predicate results
func (r *BlockResults) Bytes() ([]byte, error) {
	// Convert to wire representation before marshaling to avoid serializing
	// set.Bits directly, which is not supported by the reflect-based codec.
	wire := encodedBlockResults{TxResults: make(map[common.Hash]encodedPrecompileResults, len(r.TxResults))}
	for txHash, addrToBits := range r.TxResults {
		enc := make(encodedPrecompileResults, len(addrToBits))
		for addr, bits := range addrToBits {
			enc[addr] = bits.Bytes()
		}
		wire.TxResults[txHash] = enc
	}
	return resultsCodec.Marshal(version, &wire)
}
