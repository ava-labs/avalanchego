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
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	version        = uint16(0)
	MaxResultsSize = units.MiB
)

var resultsCodec codec.Manager

func init() {
	resultsCodec = codec.NewManager(MaxResultsSize)

	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		resultsCodec.RegisterCodec(version, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// TxResults is a map of results for each precompile address to the resulting byte array.
type TxResults map[common.Address][]byte

// Results encodes the precompile predicate results included in a block on a per transaction basis.
// Results is not thread-safe.
type Results struct {
	Results map[common.Hash]TxResults `serialize:"true"`
}

// NewResults returns an empty predicate results.
func NewResults() *Results {
	return &Results{
		Results: make(map[common.Hash]TxResults),
	}
}

func NewResultsFromMap(results map[common.Hash]TxResults) *Results {
	return &Results{
		Results: results,
	}
}

// ParseResults parses [b] into predicate results.
func ParseResults(b []byte) (*Results, error) {
	res := new(Results)
	_, err := resultsCodec.Unmarshal(b, res)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}
	return res, nil
}

// GetResults returns the byte array results for [txHash] from precompile [address] if available.
func (r *Results) GetResults(txHash common.Hash, address common.Address) []byte {
	txResults, ok := r.Results[txHash]
	if !ok {
		return nil
	}
	return txResults[address]
}

// SetTxResults sets the predicate results for the given [txHash]. Overrides results if present.
func (r *Results) SetTxResults(txHash common.Hash, txResults TxResults) {
	// If there are no tx results, don't store an entry in the map
	if len(txResults) == 0 {
		delete(r.Results, txHash)
		return
	}
	r.Results[txHash] = txResults
}

// DeleteTxResults deletes the predicate results for the given [txHash].
func (r *Results) DeleteTxResults(txHash common.Hash) {
	delete(r.Results, txHash)
}

// Bytes marshals the current state of predicate results
func (r *Results) Bytes() ([]byte, error) {
	return resultsCodec.Marshal(version, r)
}

func (r *Results) String() string {
	sb := strings.Builder{}

	fmt.Fprintf(&sb, "PredicateResults: (Size = %d)", len(r.Results))
	for txHash, results := range r.Results {
		for address, result := range results {
			fmt.Fprintf(&sb, "\n%s    %s: %x", txHash, address, result)
		}
	}

	return sb.String()
}
