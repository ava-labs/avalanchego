// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errInvalidSubnetAuth = errors.New("invalid subnet auth")

func TestRemoveSubnetValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		name        string
		txFunc      func(*gomock.Controller) *RemoveSubnetValidatorTx
		expectedErr error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:   chainID,
		NetworkID: networkID,
	}

	// A BaseTx that already passed syntactic verification.
	verifiedBaseTx := BaseTx{
		SyntacticallyVerified: true,
	}
	// Sanity check.
	require.NoError(t, verifiedBaseTx.SyntacticVerify(ctx))

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}
	// Sanity check.
	require.NoError(t, validBaseTx.SyntacticVerify(ctx))
	// Make sure we're not caching the verification result.
	require.False(t, validBaseTx.SyntacticallyVerified)

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *RemoveSubnetValidatorTx {
				return nil
			},
			expectedErr: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *RemoveSubnetValidatorTx {
				return &RemoveSubnetValidatorTx{BaseTx: verifiedBaseTx}
			},
			expectedErr: nil,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *RemoveSubnetValidatorTx {
				return &RemoveSubnetValidatorTx{
					// Set subnetID so we don't error on that check.
					Subnet: ids.GenerateTestID(),
					// Set NodeID so we don't error on that check.
					NodeID: ids.GenerateTestNodeID(),
					BaseTx: invalidBaseTx,
				}
			},
			expectedErr: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid subnetID",
			txFunc: func(*gomock.Controller) *RemoveSubnetValidatorTx {
				return &RemoveSubnetValidatorTx{
					BaseTx: validBaseTx,
					// Set NodeID so we don't error on that check.
					NodeID: ids.GenerateTestNodeID(),
					Subnet: constants.PrimaryNetworkID,
				}
			},
			expectedErr: ErrRemovePrimaryNetworkValidator,
		},
		{
			name: "invalid subnetAuth",
			txFunc: func(ctrl *gomock.Controller) *RemoveSubnetValidatorTx {
				// This SubnetAuth fails verification.
				invalidSubnetAuth := verify.NewMockVerifiable(ctrl)
				invalidSubnetAuth.EXPECT().Verify().Return(errInvalidSubnetAuth)
				return &RemoveSubnetValidatorTx{
					// Set subnetID so we don't error on that check.
					Subnet: ids.GenerateTestID(),
					// Set NodeID so we don't error on that check.
					NodeID:     ids.GenerateTestNodeID(),
					BaseTx:     validBaseTx,
					SubnetAuth: invalidSubnetAuth,
				}
			},
			expectedErr: errInvalidSubnetAuth,
		},
		{
			name: "passes verification",
			txFunc: func(ctrl *gomock.Controller) *RemoveSubnetValidatorTx {
				// This SubnetAuth passes verification.
				validSubnetAuth := verify.NewMockVerifiable(ctrl)
				validSubnetAuth.EXPECT().Verify().Return(nil)
				return &RemoveSubnetValidatorTx{
					// Set subnetID so we don't error on that check.
					Subnet: ids.GenerateTestID(),
					// Set NodeID so we don't error on that check.
					NodeID:     ids.GenerateTestNodeID(),
					BaseTx:     validBaseTx,
					SubnetAuth: validSubnetAuth,
				}
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr == nil {
				require.True(tx.SyntacticallyVerified)
			}
		})
	}
}
