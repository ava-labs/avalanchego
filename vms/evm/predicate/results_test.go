// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestBlockResultsParsing(t *testing.T) {
	tests := []struct {
		name    string
		results map[common.Hash]PrecompileResults
		wantHex string
	}{
		{
			name:    "empty",
			results: make(map[common.Hash]PrecompileResults),
			wantHex: "000000000000",
		},
		{
			name: "single tx no results",
			results: map[common.Hash]PrecompileResults{
				{1}: {},
			},
			wantHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name: "single tx single result",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
			},
			wantHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203",
		},
		{
			name: "single tx multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {1, 2, 3},
				},
			},
			wantHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003010203",
		},
		{
			name: "multiple txs no result",
			results: map[common.Hash]PrecompileResults{
				{1}: {},
				{2}: {},
			},
			wantHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name: "multiple txs single result",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
				{2}: map[common.Address][]byte{
					{3}: {3, 2, 1},
				},
			},
			wantHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203020000000000000000000000000000000000000000000000000000000000000000000001030000000000000000000000000000000000000000000003030201",
		},
		{
			name: "multiple txs multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
				{2}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
			},
			wantHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201020000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201",
		},
		{
			name: "multiple txs mixed results",
			results: map[common.Hash]PrecompileResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
				{2}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
				{3}: {},
			},
			wantHex: "000000000003010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203020000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201030000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			predicateResults := BlockResults{TxResults: tt.results}
			b, err := predicateResults.Bytes()
			require.NoError(err)
			require.Equal(tt.wantHex, common.Bytes2Hex(b))

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
	predicateResult := []byte{1, 2, 3}
	txPredicateResults := map[common.Address][]byte{
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

func TestBlockResultsDelete(t *testing.T) {
	require := require.New(t)

	predicateResults := BlockResults{}

	txHash := common.Hash{1}
	addr := common.Address{2}
	predicateResult := []byte{1, 2, 3}
	txPredicateResults := map[common.Address][]byte{
		addr: predicateResult,
	}

	// Set up some results
	predicateResults.Set(txHash, txPredicateResults)
	result, ok := predicateResults.Get(txHash, addr)
	require.True(ok)
	require.Equal(predicateResult, result)

	// Test Delete
	predicateResults.Delete(txHash)
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
	require.Equal("PredicateResults: (Size = 0)", results.String())

	// Should be able to set results without panicking
	results.Set(common.Hash{1}, PrecompileResults{
		common.Address{2}: {1, 2, 3},
	})

	// Should now return the results
	result, ok = results.Get(common.Hash{1}, common.Address{2})
	require.True(ok)
	require.Equal([]byte{1, 2, 3}, result)
	require.Contains(results.String(), "Size = 1")
}
