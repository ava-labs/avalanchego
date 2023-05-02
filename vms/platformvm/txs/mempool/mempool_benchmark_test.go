// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// Benchmarks marshal-ing "Version" message.
//
// e.g.,
//
//	$ go install -v golang.org/x/tools/cmd/benchcmp@latest
//	$ go install -v golang.org/x/perf/cmd/benchstat@latest
//
//	$ go test -run=NONE -bench=BenchmarkPeekTxs > /tmp/cpu.before.txt
//	$ go test -run=NONE -bench=BenchmarkPeekTxs > /tmp/cpu.after.txt
//	$ benchcmp /tmp/cpu.before.txt /tmp/cpu.after.txt
//	$ benchstat -alpha 0.03 /tmp/cpu.before.txt /tmp/cpu.after.txt
//
//	$ go test -run=NONE -bench=BenchmarkPeekTxs -benchmem > /tmp/mem.before.txt
//	$ go test -run=NONE -bench=BenchmarkPeekTxs -benchmem > /tmp/mem.after.txt
//	$ benchcmp /tmp/mem.before.txt /tmp/mem.after.txt
//	$ benchstat -alpha 0.03 /tmp/mem.before.txt /tmp/mem.after.txt
func BenchmarkPeekTxs(b *testing.B) {
	require := require.New(b)

	registerer := prometheus.NewRegistry()
	mpool, err := NewMempool("mempool", registerer, &noopBlkTimer{})
	require.NoError(err)

	total := 300
	decisionTxs, err := createTestDecisionTxs(total)
	require.NoError(err)
	stakerTxs, err := createTestAddPermissionlessValidatorTxs(total)
	require.NoError(err)

	// txs must not already there before we start
	require.False(mpool.HasTxs())

	oneDecisionTxSize, totalDecisionTxSize := 0, 0
	for _, tx := range decisionTxs {
		require.False(mpool.Has(tx.ID()))
		require.NoError(mpool.Add(tx))

		size := tx.Size()
		if oneDecisionTxSize != 0 {
			// assume all txs have the same size for the purpose of testing
			require.Equal(oneDecisionTxSize, size)
		} else {
			oneDecisionTxSize = size
		}

		totalDecisionTxSize += oneDecisionTxSize
	}

	oneStakerTxSize, totalStakerTxSize := 0, 0
	for _, tx := range stakerTxs {
		require.False(mpool.Has(tx.ID()))
		require.NoError(mpool.Add(tx))

		size := tx.Size()
		if oneStakerTxSize != 0 {
			// assume all txs have the same size for the purpose of testing
			require.Equal(oneStakerTxSize, size)
		} else {
			oneStakerTxSize = size
		}

		totalStakerTxSize += oneStakerTxSize
	}

	// reasonable limit to both query decision txs + staker txs
	maxTxBytes := totalDecisionTxSize + totalStakerTxSize/2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mpool.PeekTxs(maxTxBytes)
	}
}
