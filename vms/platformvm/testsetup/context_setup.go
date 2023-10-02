// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var errMissing = errors.New("missing")

type MutableSharedMemory struct {
	atomic.SharedMemory
}

func Context(r *require.Assertions, baseDB database.Database) (*snow.Context, *MutableSharedMemory) {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = NetworkID
	ctx.XChainID = XChainID
	ctx.CChainID = CChainID
	ctx.AVAXAssetID = AvaxAssetID

	aliaser := ids.NewAliaser()
	r.NoError(aliaser.Alias(constants.PlatformChainID, "P"))
	r.NoError(aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()))
	r.NoError(aliaser.Alias(XChainID, "X"))
	r.NoError(aliaser.Alias(XChainID, XChainID.String()))
	r.NoError(aliaser.Alias(CChainID, "C"))
	r.NoError(aliaser.Alias(CChainID, CChainID.String()))
	ctx.BCLookup = aliaser

	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)

	msm := &MutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				XChainID:                  constants.PrimaryNetworkID,
				CChainID:                  constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errMissing
			}
			return subnetID, nil
		},
	}

	return ctx, msm
}
