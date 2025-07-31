// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestPredicateResultsParsing(t *testing.T) {
	type test struct {
		results     map[common.Hash]TxResults
		expectedHex string
	}
	for name, test := range map[string]test{
		"empty": {
			results:     make(map[common.Hash]TxResults),
			expectedHex: "000000000000",
		},
		"single tx no results": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{},
			},
			expectedHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000000",
		},
		"single tx single result": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
			},
			expectedHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203",
		},
		"single tx multiple results": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {1, 2, 3},
				},
			},
			expectedHex: "000000000001010000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003010203",
		},
		"multiple txs no result": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{},
				{2}: map[common.Address][]byte{},
			},
			expectedHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000",
		},
		"multiple txs single result": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
				{2}: map[common.Address][]byte{
					{3}: {3, 2, 1},
				},
			},
			expectedHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203020000000000000000000000000000000000000000000000000000000000000000000001030000000000000000000000000000000000000000000003030201",
		},
		"multiple txs multiple results": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
				{2}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
			},
			expectedHex: "000000000002010000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201020000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201",
		},
		"multiple txs mixed results": {
			results: map[common.Hash]TxResults{
				{1}: map[common.Address][]byte{
					{2}: {1, 2, 3},
				},
				{2}: map[common.Address][]byte{
					{2}: {1, 2, 3},
					{3}: {3, 2, 1},
				},
				{3}: map[common.Address][]byte{},
			},
			expectedHex: "000000000003010000000000000000000000000000000000000000000000000000000000000000000001020000000000000000000000000000000000000000000003010203020000000000000000000000000000000000000000000000000000000000000000000002020000000000000000000000000000000000000000000003010203030000000000000000000000000000000000000000000003030201030000000000000000000000000000000000000000000000000000000000000000000000",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			predicateResults := NewResultsFromMap(test.results)
			b, err := predicateResults.Bytes()
			require.NoError(err)

			parsedPredicateResults, err := ParseResults(b)
			require.NoError(err)
			require.Equal(predicateResults, parsedPredicateResults)
			require.Equal(test.expectedHex, common.Bytes2Hex(b))
		})
	}
}

func TestPredicateResultsAccessors(t *testing.T) {
	require := require.New(t)

	predicateResults := NewResults()

	txHash := common.Hash{1}
	addr := common.Address{2}
	predicateResult := []byte{1, 2, 3}
	txPredicateResults := map[common.Address][]byte{
		addr: predicateResult,
	}

	require.Empty(predicateResults.GetResults(txHash, addr))
	predicateResults.SetTxResults(txHash, txPredicateResults)
	require.Equal(predicateResult, predicateResults.GetResults(txHash, addr))
	predicateResults.DeleteTxResults(txHash)
	require.Empty(predicateResults.GetResults(txHash, addr))

	// Ensure setting empty tx predicate results removes the entry
	predicateResults.SetTxResults(txHash, txPredicateResults)
	require.Equal(predicateResult, predicateResults.GetResults(txHash, addr))
	predicateResults.SetTxResults(txHash, map[common.Address][]byte{})
	require.Empty(predicateResults.GetResults(txHash, addr))
}
