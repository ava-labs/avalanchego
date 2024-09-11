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
)

func TestEngineStragglerDetector(t *testing.T) {
	require := require.New(t)

	fakeClock := make(chan time.Time, 1)

	conf := DefaultConfig(t)
	peerID, _, sender, vm, engine := setup(t, conf)

	parent := snowmantest.BuildChild(snowmantest.Genesis)
	require.NoError(conf.Consensus.Add(parent))

	listenerShouldInvokeWith := []time.Duration{0, 0, time.Second * 2}

	fakeTime := func() time.Time {
		select {
		case now := <-fakeClock:
			return now
		default:
			require.Fail("should have a time.Time in the channel")
			return time.Time{}
		}
	}

	f := func(duration time.Duration) {
		require.Equal(listenerShouldInvokeWith[0], duration)
		listenerShouldInvokeWith = listenerShouldInvokeWith[1:]
	}

	decoratedEngine := NewDecoratedEngine(engine, fakeTime, f)

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
	fakeClock <- now
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID()))
	now = now.Add(time.Second * 2)
	fakeClock <- now
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID()))
	now = now.Add(time.Second * 2)
	fakeClock <- now
	require.NoError(decoratedEngine.Chits(context.Background(), peerID, 0, parent.ID(), parent.ID(), parent.ID()))
	require.Empty(listenerShouldInvokeWith)
}
