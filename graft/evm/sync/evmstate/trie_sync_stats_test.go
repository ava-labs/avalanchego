// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/metrics"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestETAShouldNotOverflow(t *testing.T) {
	require := require.New(t)
	now := time.Now()
	start := now.Add(-6 * time.Hour)

	stats := &trieSyncStats{
		log:            logging.NoLog{},
		triesStartTime: start,
		triesSynced:    100_000,
		triesRemaining: 450_000,
		leafsRateGauge: metrics.NilGauge{},
	}
	require.Positive(stats.updateETA(time.Minute, now))
}
