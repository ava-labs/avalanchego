// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package results

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/common"
)

const (
	Version        = uint16(0)
	MaxResultsSize = units.MiB
)

var Codec codec.Manager

func init() {
	Codec = codec.NewManager(MaxResultsSize)

	c := linearcodec.NewDefault()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(PredicateResults{}),
		Codec.RegisterCodec(Version, c),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// TxPredicateResults is a map of results for each precompile address to the resulting byte array.
type TxPredicateResults map[common.Address][]byte

// PredicateResults encodes the precompile predicate results included in a block on a per transaction basis.
// PredicateResults is not thread-safe.
type PredicateResults struct {
	Results map[common.Hash]TxPredicateResults `serialize:"true"`
}

// NewPredicateResults returns an empty predicate results.
func NewPredicateResults() *PredicateResults {
	return &PredicateResults{
		Results: make(map[common.Hash]TxPredicateResults),
	}
}

func NewPredicateResultsFromMap(results map[common.Hash]TxPredicateResults) *PredicateResults {
	return &PredicateResults{
		Results: results,
	}
}

// ParsePredicateResults parses [b] into predicate results.
func ParsePredicateResults(b []byte) (*PredicateResults, error) {
	res := new(PredicateResults)
	parsedVersion, err := Codec.Unmarshal(b, res)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}
	if parsedVersion != Version {
		return nil, fmt.Errorf("invalid version (found %d, expected %d)", parsedVersion, Version)
	}
	return res, nil
}

// GetPredicateResults returns the byte array results for [txHash] from precompile [address] if available.
func (p *PredicateResults) GetPredicateResults(txHash common.Hash, address common.Address) []byte {
	txResults, ok := p.Results[txHash]
	if !ok {
		return nil
	}
	return txResults[address]
}

// SetTxPredicateResults sets the predicate results for the given [txHash]. Overrides results if present.
func (p *PredicateResults) SetTxPredicateResults(txHash common.Hash, txResults TxPredicateResults) {
	// If there are no tx results, don't store an entry in the map
	if len(txResults) == 0 {
		delete(p.Results, txHash)
		return
	}
	p.Results[txHash] = txResults
}

// DeleteTxPredicateResults deletes the predicate results for the given [txHash].
func (p *PredicateResults) DeleteTxPredicateResults(txHash common.Hash) {
	delete(p.Results, txHash)
}

// Bytes marshals the current state of predicate results
func (p *PredicateResults) Bytes() ([]byte, error) {
	return Codec.Marshal(Version, p)
}

func (p *PredicateResults) String() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("PredicateResults: (Size = %d)", len(p.Results)))
	for txHash, results := range p.Results {
		for address, result := range results {
			sb.WriteString(fmt.Sprintf("\n%s    %s: %x", txHash, address, result))
		}
	}

	return sb.String()
}
