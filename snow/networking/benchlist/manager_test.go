// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type noOpBenchable struct{}

func (noOpBenchable) Benched(ids.ID, ids.NodeID)   {}
func (noOpBenchable) Unbenched(ids.ID, ids.NodeID) {}

func TestManagerShutdownStopsChainBenchlists(t *testing.T) {
	require := require.New(t)

	snowCtx0 := snowtest.Context(t, ids.GenerateTestID())
	ctx0 := snowtest.ConsensusContext(snowCtx0)
	ctx0.PrimaryAlias = "chain0"

	snowCtx1 := snowtest.Context(t, ids.GenerateTestID())
	ctx1 := snowtest.ConsensusContext(snowCtx1)
	ctx1.PrimaryAlias = "chain1"

	vdrs := validators.NewManager()
	require.NoError(vdrs.AddStaker(ctx0.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))
	require.NoError(vdrs.AddStaker(ctx1.SubnetID, ids.GenerateTestNodeID(), nil, ids.Empty, 1))

	m := NewManager(
		noOpBenchable{},
		vdrs,
		metrics.NewPrefixGatherer(),
		Config{
			Halflife:           DefaultHalflife,
			UnbenchProbability: DefaultUnbenchProbability,
			BenchProbability:   DefaultBenchProbability,
			BenchDuration:      DefaultBenchDuration,
			MaxPortion:         0.999,
		},
	).(*manager)
	require.NoError(m.RegisterChain(ctx0))
	require.NoError(m.RegisterChain(ctx1))

	m.lock.RLock()
	chainBenchlists := make([]*benchlist, 0, len(m.chains))
	for _, chainBenchlist := range m.chains {
		chainBenchlists = append(chainBenchlists, chainBenchlist)
	}
	m.lock.RUnlock()
	require.Len(chainBenchlists, 2)

	m.Shutdown()
	m.Shutdown()

	m.lock.RLock()
	require.True(m.shutdown)
	require.Empty(m.chains)
	m.lock.RUnlock()
}
