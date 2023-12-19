// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	errMissing = errors.New("missing")

	_ snow.Acceptor = noOpAcceptor{}
)

type noOpAcceptor struct{}

func (noOpAcceptor) Accept(*snow.ConsensusContext, ids.ID, []byte) error {
	return nil
}

func ConsensusContext() *snow.ConsensusContext {
	return &snow.ConsensusContext{
		Context:             snow.DefaultContextTest(),
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
		BlockAcceptor:       noOpAcceptor{},
		TxAcceptor:          noOpAcceptor{},
		VertexAcceptor:      noOpAcceptor{},
	}
}

func Context(tb testing.TB) *snow.Context {
	require := require.New(tb)

	ctx := snow.DefaultContextTest()
	ctx.NetworkID = constants.UnitTestID
	ctx.XChainID = ids.GenerateTestID()
	ctx.CChainID = ids.GenerateTestID()
	ctx.AVAXAssetID = ids.GenerateTestID()
	aliaser := ctx.BCLookup.(ids.Aliaser)
	require.NoError(aliaser.Alias(constants.PlatformChainID, "P"))
	require.NoError(aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()))

	require.NoError(aliaser.Alias(ctx.XChainID, "X"))
	require.NoError(aliaser.Alias(ctx.XChainID, ctx.XChainID.String()))

	require.NoError(aliaser.Alias(ctx.CChainID, "C"))
	require.NoError(aliaser.Alias(ctx.CChainID, ctx.CChainID.String()))

	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				ctx.XChainID:              constants.PrimaryNetworkID,
				ctx.CChainID:              constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errMissing
			}
			return subnetID, nil
		},
	}

	return ctx
}
