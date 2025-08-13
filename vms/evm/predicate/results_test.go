// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestBlockResultsParsing(t *testing.T) {
	tests := []struct {
		name    string
		results map[common.Hash]PrecompileResults
	}{
		{
			name:    "empty",
			results: make(map[common.Hash]PrecompileResults),
		},
		{
			name: "single tx no results",
			results: map[common.Hash]PrecompileResults{
				{1}: {},
			},
		},
		{
			name: "single tx single result",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name: "single tx multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name: "multiple txs no result",
			results: map[common.Hash]PrecompileResults{
				{1}: {},
				{2}: {},
			},
		},
		{
			name: "multiple txs single result",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
				},
				{2}: map[common.Address]set.Bits{
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
		{
			name: "multiple txs multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
				{2}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
		{
			name: "multiple txs mixed results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
				},
				{2}: map[common.Address]set.Bits{
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
				{3}: {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			predicateResults := BlockResults{TxResults: tt.results}
			b, err := predicateResults.Bytes()
			require.NoError(err)
			require.Equal(expectedHexFromResults(tt.results), common.Bytes2Hex(b))

			parsedPredicateResults, err := ParseBlockResults(b)
			require.NoError(err)
			require.Equal(predicateResults, parsedPredicateResults)
		})
	}
}

func TestBlockResultsGetSet(t *testing.T) {
	require := require.New(t)

	// Test using the meaningful zero value
	predicateResults := BlockResults{}

	txHash := common.Hash{1}
	addr := common.Address{2}
	predicateResult := set.NewBits(1, 2, 3)
	txPredicateResults := map[common.Address]set.Bits{
		addr: predicateResult,
	}

	// Test Get on empty results
	result, ok := predicateResults.Get(txHash, addr)
	require.False(ok)
	require.Empty(result)

	// Test Set and Get
	predicateResults.Set(txHash, txPredicateResults)
	result, ok = predicateResults.Get(txHash, addr)
	require.True(ok)
	require.Equal(predicateResult, result)

	// Test setting empty results
	predicateResults.Set(txHash, PrecompileResults{})
	result, ok = predicateResults.Get(txHash, addr)
	require.False(ok)
	require.Empty(result)
}

func TestBlockResultsZeroValue(t *testing.T) {
	require := require.New(t)

	// Test that BlockResults{} has a meaningful zero value
	var results BlockResults

	// Should not panic and should return empty results
	result, ok := results.Get(common.Hash{1}, common.Address{2})
	require.False(ok)
	require.Empty(result)
	require.Empty(results.TxResults)

	// Should be able to set results without panicking
	results.Set(common.Hash{1}, PrecompileResults{
		common.Address{2}: set.NewBits(1, 2, 3),
	})

	// Should now return the results
	result, ok = results.Get(common.Hash{1}, common.Address{2})
	require.True(ok)
	require.Equal(set.NewBits(1, 2, 3), result)
	require.Len(results.TxResults, 1)
}

func TestBlockResultsNilResultsBytes(t *testing.T) {
	require := require.New(t)

	// Test that BlockResults with nil TxResults can be marshaled without panicking
	var results BlockResults
	require.Nil(results.TxResults)

	// Should not panic and should return valid bytes
	b, err := results.Bytes()
	require.NoError(err)
	require.NotNil(b)

	// Should be able to parse the bytes back
	parsedResults, err := ParseBlockResults(b)
	require.NoError(err)
	// Note: nil maps get converted to empty maps during marshaling/unmarshaling
	require.Empty(parsedResults.TxResults)
	// The original results should still be nil
	require.Nil(results.TxResults)
}

// expectedHexFromResults deterministically computes the expected hex encoding
// for the given results using the same on-wire representation as production
// code (i.e., []byte values for bitsets).
func expectedHexFromResults(results map[common.Hash]PrecompileResults) string {
	wire := encodedBlockResults{TxResults: make(map[common.Hash]encodedPrecompileResults, len(results))}
	for txHash, addrToBits := range results {
		enc := make(encodedPrecompileResults, len(addrToBits))
		for addr, bits := range addrToBits {
			enc[addr] = bits.Bytes()
		}
		wire.TxResults[txHash] = enc
	}
	b, _ := resultsCodec.Marshal(version, &wire)
	return common.Bytes2Hex(b)
}
