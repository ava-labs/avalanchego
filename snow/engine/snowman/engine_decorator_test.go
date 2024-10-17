// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestEngineStragglerDetector(t *testing.T) {
	require := require.New(t)

	var fakeClock mockable.Clock

	conf := DefaultConfig(t)
	peerID, _, sender, vm, engine := setup(t, conf)

	parent := snowmantest.BuildChild(snowmantest.Genesis)
	require.NoError(conf.Consensus.Add(parent))

	listenerShouldInvokeWith := []time.Duration{0, 0, minStragglerCheckInterval * 2}

	f := func(duration time.Duration) bool {
		require.Equal(listenerShouldInvokeWith[0], duration)
		listenerShouldInvokeWith = listenerShouldInvokeWith[1:]
		return false
	}

	decoratedEngine := NewDecoratedEngineWithStragglerDetector(engine, fakeClock.Time, f)

	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {
	}
	vm.ParseBlockF = func(_ context.Context, _ []byte) (snowman.Block, error) {
		require.FailNow("should not be called")
		return nil, nil
	}

	now := time.Now()
	fakeClock.Set(now)
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID(), 100))
	now = now.Add(minStragglerCheckInterval * 2)
	fakeClock.Set(now)
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID(), 100))
	now = now.Add(minStragglerCheckInterval * 2)
	fakeClock.Set(now)
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID(), 100))
	require.Empty(listenerShouldInvokeWith)
}
