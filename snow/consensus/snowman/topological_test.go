// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: snowball.SnowflakeFactory})
}

func TestTopologicalHealthCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := snowtest.ConsensusContext(snowtest.Context(t, snowtest.CChainID))
		params := snowball.DefaultParameters
		sm := TopologicalFactory{factory: snowball.SnowflakeFactory}.New()
		require.NoErrorf(
			t,
			sm.Initialize(
				ctx,
				params,
				snowmantest.GenesisID,
				snowmantest.GenesisHeight,
				snowmantest.GenesisTimestamp,
			),
			"%T.Initialize()",
			sm,
		)

		// blk1 is newer than blk0, so it shows that the oldest block is
		// reported.
		blk0 := snowmantest.BuildChild(snowmantest.Genesis)
		blk1 := snowmantest.BuildChild(blk0)
		require.NoErrorf(t, sm.Add(blk0), "%T.Add(blk0)", sm)
		require.NoErrorf(t, sm.Add(blk1), "%T.Add(blk1)", sm)

		elapsed := params.MaxItemProcessingTime + time.Second
		time.Sleep(elapsed)

		details, err := sm.HealthCheck(t.Context())
		require.ErrorIsf(t, err, errBlockProcessingTooLong, "%T.HealthCheck()", sm)
		want := map[string]interface{}{
			"processingBlocks":             2,
			"longestProcessingBlock":       elapsed.String(),
			"longestProcessingBlockID":     blk0.ID(),
			"longestProcessingBlockHeight": blk0.Height(),
			"lastAcceptedID":               snowmantest.GenesisID,
			"lastAcceptedHeight":           snowmantest.GenesisHeight,
		}
		require.Equalf(t, want, details, "%T.HealthCheck()", sm)
	})
}
