// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"fmt"
	"strings"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	version        = uint16(0)
	maxResultsSize = units.MiB
)

var resultsCodec codec.Manager

func init() {
	resultsCodec = codec.NewManager(maxResultsSize)

	c := linearcodec.NewDefault()
	err := resultsCodec.RegisterCodec(version, c)
	if err != nil {
		panic(err)
	}
}

// PrecompileResults is a map of results for each precompile address to the resulting byte array.
type PrecompileResults map[common.Address][]byte

// BlockResults encodes the precompile predicate results included in a block on a per transaction basis.
// BlockResults is not thread-safe.
type BlockResults struct {
	TxResults map[common.Hash]PrecompileResults `serialize:"true"`
}

// NewBlockResults returns an empty predicate results.
func NewBlockResults() BlockResults {
	return BlockResults{
		TxResults: make(map[common.Hash]PrecompileResults),
	}
}

// ParseBlockResults parses bytes into predicate results.
func ParseBlockResults(b []byte) (BlockResults, error) {
	res := new(BlockResults)
	_, err := resultsCodec.Unmarshal(b, res)
	if err != nil {
		return BlockResults{}, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}

	return *res, nil
}

// Get returns the byte array results for txHash from precompile address if available.
// Returns (nil, false) if the txHash or address is not found.
func (r *BlockResults) Get(txHash common.Hash, address common.Address) ([]byte, bool) {
	if r.TxResults == nil {
		return nil, false
	}
	txResults, ok := r.TxResults[txHash]
	if !ok {
		return nil, false
	}

	result, ok := txResults[address]
	return result, ok
}

// Set sets the predicate results for the given txHash. Overrides results if present.
func (r *BlockResults) Set(txHash common.Hash, txResults PrecompileResults) {
	if r.TxResults == nil {
		r.TxResults = make(map[common.Hash]PrecompileResults)
	}
	r.TxResults[txHash] = txResults
}

// Delete deletes the predicate results for the given txHash.
func (r *BlockResults) Delete(txHash common.Hash) {
	if r.TxResults == nil {
		return
	}
	delete(r.TxResults, txHash)
}

// Bytes marshals the current state of predicate results
func (r *BlockResults) Bytes() ([]byte, error) {
	return resultsCodec.Marshal(version, r)
}

func (r *BlockResults) String() string {
	sb := strings.Builder{}

	if r.TxResults == nil {
		fmt.Fprintf(&sb, "PredicateResults: (Size = 0)")
		return sb.String()
	}

	fmt.Fprintf(&sb, "PredicateResults: (Size = %d)", len(r.TxResults))
	for txHash, results := range r.TxResults {
		for address, result := range results {
			fmt.Fprintf(&sb, "\n%s    %s: %x", txHash, address, result)
		}
	}

	return sb.String()
}
