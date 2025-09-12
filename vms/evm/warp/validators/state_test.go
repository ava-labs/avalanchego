// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGetValidatorSetPrimaryNetwork(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	mySubnetID := ids.GenerateTestID()
	otherSubnetID := ids.GenerateTestID()

	mockState := validatorsmock.NewState(ctrl)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.SubnetID = mySubnetID
	snowCtx.ValidatorState = mockState
	state := NewState(snowCtx.ValidatorState, snowCtx.SubnetID, snowCtx.ChainID, false)
	// Expect that requesting my validator set returns my validator set
	mockState.EXPECT().GetValidatorSet(gomock.Any(), gomock.Any(), mySubnetID).Return(make(map[ids.NodeID]*validators.GetValidatorOutput), nil)
	output, err := state.GetValidatorSet(context.Background(), 10, mySubnetID)
	require.NoError(err)
	require.Len(output, 0)

	// Expect that requesting the Primary Network validator set overrides and returns my validator set
	mockState.EXPECT().GetValidatorSet(gomock.Any(), gomock.Any(), mySubnetID).Return(make(map[ids.NodeID]*validators.GetValidatorOutput), nil)
	output, err = state.GetValidatorSet(context.Background(), 10, constants.PrimaryNetworkID)
	require.NoError(err)
	require.Len(output, 0)

	// Expect that requesting other validator set returns that validator set
	mockState.EXPECT().GetValidatorSet(gomock.Any(), gomock.Any(), otherSubnetID).Return(make(map[ids.NodeID]*validators.GetValidatorOutput), nil)
	output, err = state.GetValidatorSet(context.Background(), 10, otherSubnetID)
	require.NoError(err)
	require.Len(output, 0)
}
