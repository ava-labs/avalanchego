// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var errMissing = errors.New("missing")

func TestSameSubnet(t *testing.T) {
	subnetID0 := ids.GenerateTestID()
	subnetID1 := ids.GenerateTestID()
	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	tests := []struct {
		name    string
		ctxF    func(*gomock.Controller) *snow.Context
		chainID ids.ID
		result  error
	}{
		{
			name: "same chain",
			ctxF: func(ctrl *gomock.Controller) *snow.Context {
				state := validators.NewMockState(ctrl)
				return &snow.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID0,
			result:  ErrSameChainID,
		},
		{
			name: "unknown chain",
			ctxF: func(ctrl *gomock.Controller) *snow.Context {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, errMissing)
				return &snow.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  errMissing,
		},
		{
			name: "wrong subnet",
			ctxF: func(ctrl *gomock.Controller) *snow.Context {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID1, nil)
				return &snow.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  ErrMismatchedSubnetIDs,
		},
		{
			name: "same subnet",
			ctxF: func(ctrl *gomock.Controller) *snow.Context {
				state := validators.NewMockState(ctrl)
				state.EXPECT().GetSubnetID(gomock.Any(), chainID1).Return(subnetID0, nil)
				return &snow.Context{
					SubnetID:       subnetID0,
					ChainID:        chainID0,
					ValidatorState: state,
				}
			},
			chainID: chainID1,
			result:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			ctx := test.ctxF(ctrl)

			result := SameSubnet(context.Background(), ctx, test.chainID)
			require.ErrorIs(t, result, test.result)
		})
	}
}
