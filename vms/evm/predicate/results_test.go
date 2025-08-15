// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestParseBlockResults(t *testing.T) {
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
				{1}: {
					{2}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name: "single tx multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: {
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
				{1}: {
					{2}: set.NewBits(1, 2, 3),
				},
				{2}: {
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
		{
			name: "multiple txs multiple results",
			results: map[common.Hash]PrecompileResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
				{2}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
		{
			name: "multiple txs mixed results",
			results: map[common.Hash]PrecompileResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
				},
				{2}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
				{3}: {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := require.New(t)
			// Build bytes for the encoded representation directly
			enc := encodedBlockResults{TxResults: make(map[common.Hash]map[common.Address][]byte, len(tt.results))}
			for tx, addrToBits := range tt.results {
				addrToBytes := make(map[common.Address][]byte, len(addrToBits))
				for addr, bits := range addrToBits {
					addrToBytes[addr] = bits.Bytes()
				}
				enc.TxResults[tx] = addrToBytes
			}
			bytes, err := resultsCodec.Marshal(version, &enc)
			req.NoError(err)

			got, err := ParseBlockResults(bytes)
			req.NoError(err)
			req.Equal(BlockResults{TxResults: tt.results}, got)
		})
	}
}

func TestBlockResultsGetSet(t *testing.T) {
	type setArgs struct {
		tx      common.Hash
		results PrecompileResults
	}
	type getArgs struct {
		tx       common.Hash
		addr     common.Address
		wantBits set.Bits
		wantOK   bool
	}
	type vecCase struct {
		name    string
		sets    []setArgs
		gets    []getArgs
		wantLen int
	}

	tests := []vecCase{
		{
			name:    "zero value no sets",
			sets:    nil,
			gets:    []getArgs{{tx: common.Hash{1}, addr: common.Address{2}, wantBits: set.Bits{}, wantOK: false}},
			wantLen: 0,
		},
		{
			name: "single set and get",
			sets: []setArgs{{
				tx: common.Hash{1},
				results: PrecompileResults{
					common.Address{2}: set.NewBits(1, 2, 3),
				},
			}},
			gets: []getArgs{{
				tx:       common.Hash{1},
				addr:     common.Address{2},
				wantBits: set.NewBits(1, 2, 3),
				wantOK:   true,
			}},
			wantLen: 1,
		},
		{
			name: "multiple txs and gets",
			sets: []setArgs{
				{
					tx: common.Hash{1},
					results: PrecompileResults{
						common.Address{2}: set.NewBits(1, 2, 3),
					},
				},
				{
					tx: common.Hash{2},
					results: PrecompileResults{
						common.Address{3}: set.NewBits(3, 2, 1),
					},
				},
			},
			gets: []getArgs{
				{tx: common.Hash{1}, addr: common.Address{2}, wantBits: set.NewBits(1, 2, 3), wantOK: true},
				{tx: common.Hash{2}, addr: common.Address{3}, wantBits: set.NewBits(3, 2, 1), wantOK: true},
				{tx: common.Hash{1}, addr: common.Address{3}, wantBits: set.Bits{}, wantOK: false},
			},
			wantLen: 2,
		},
		{
			name: "overwrite with empty clears get but retains tx entry",
			sets: []setArgs{
				{
					tx: common.Hash{1},
					results: PrecompileResults{
						common.Address{2}: set.NewBits(1, 2, 3),
					},
				},
				{tx: common.Hash{1}, results: PrecompileResults{}},
			},
			gets: []getArgs{
				{tx: common.Hash{1}, addr: common.Address{2}, wantBits: set.Bits{}, wantOK: false},
			},
			wantLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			var br BlockResults
			for _, s := range tc.sets {
				br.Set(s.tx, s.results)
			}
			for _, g := range tc.gets {
				got, ok := br.Get(g.tx, g.addr)
				req.Equal(g.wantOK, ok)
				req.Equal(g.wantBits, got)
			}
			req.Len(br.TxResults, tc.wantLen)
		})
	}
}

func TestBlockResultsNilResultsBytes(t *testing.T) {
	require := require.New(t)

	// Test that BlockResults with nil TxResults can be marshaled
	var results BlockResults
	require.Nil(results.TxResults)
	b, err := results.Bytes()
	require.NoError(err)
	require.NotNil(b)
	parsedResults, err := ParseBlockResults(b)
	require.NoError(err)

	// Note: nil maps get converted to empty maps during marshaling/unmarshaling
	require.Empty(parsedResults.TxResults)
	// The original results should still be nil
	require.Nil(results.TxResults)
}
