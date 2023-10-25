// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var errMissing = errors.New("missing")

func TestSameSubnet(t *testing.T) {
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	tests := []struct {
		name string
		ctxF func(*gomock.Controller) (
			validatorState validators.State,
			subnetID ids.ID,
			chainID ids.ID,
		)
		chainID ids.ID
		result  error
	}{
		{
			name: "same chain",
			ctxF: func(ctrl *gomock.Controller) (validators.State, ids.ID, ids.ID) {
				state := validators.NewMockState(ctrl)
				return state, subnetID0, chainID0
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			ctxF: func(ctrl *gomock.Controller) (validators.State, ids.ID, ids.ID) {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, errMissing)
				return state, subnetID0, chainID0
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctxF: func(ctrl *gomock.Controller) (validators.State, ids.ID, ids.ID) {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, nil)
				return state, subnetID0, chainID0
			},
			chainID: chainID1,
			result:  ErrMismatchedSubnetIDs,
		},
		{
			name: "same subnet",
			ctxF: func(ctrl *gomock.Controller) (validators.State, ids.ID, ids.ID) {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID0, nil)
				return state, subnetID0, chainID0
			},
			chainID: chainID1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			ctxValidatorState, ctxSubnetID, ctxChainID := test.ctxF(ctrl)

			result := SameSubnet(context.Background(), ctxValidatorState, ctxSubnetID, ctxChainID, test.chainID)
			require.ErrorIs(t, result, test.result)
		})
	}
}
