// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// A huge backlog divided by a near-zero rate must not wrap the duration.
func TestETAShouldNotOverflow(t *testing.T) {
	t.Parallel()
	now := time.Now()

	stats := newTrieSyncStats(loggingtest.New(t, logging.Debug))
	stats.leavesRate = safemath.NewAverager(math.SmallestNonzeroFloat64, leafRateHalfLife, now)
	stats.remainingLeaves = map[*stateSegment]uint64{{}: math.MaxUint64}

	require.GreaterOrEqual(t, stats.estimateSegmentsInProgressTime(), time.Duration(0))
}
