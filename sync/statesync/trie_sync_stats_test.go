// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/metrics"
	"github.com/stretchr/testify/require"
)

func TestETAShouldNotOverflow(t *testing.T) {
	require := require.New(t)
	now := time.Now()
	start := now.Add(-6 * time.Hour)

	stats := &trieSyncStats{
		triesStartTime: start,
		triesSynced:    100_000,
		triesRemaining: 450_000,
		leafsRateGauge: metrics.NilGauge{},
	}
	require.Positive(stats.updateETA(time.Minute, now))
}
