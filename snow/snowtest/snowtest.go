package snowtest

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("missing")

func NewContext(tb testing.TB) *snow.Context {
	require := require.New(tb)

	ctx := snow.DefaultContextTest()
	ctx.NetworkID = constants.UnitTestID
	ctx.ChainID = ids.GenerateTestID()
	ctx.XChainID = ctx.ChainID
	ctx.CChainID = ids.GenerateTestID()
	ctx.AVAXAssetID = ids.GenerateTestID()

	aliaser := ctx.BCLookup.(ids.Aliaser)
	require.NoError(aliaser.Alias(ctx.XChainID, "X"))
	require.NoError(aliaser.Alias(ctx.XChainID, ctx.XChainID.String()))
	require.NoError(aliaser.Alias(constants.PlatformChainID, "P"))
	require.NoError(aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()))

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
