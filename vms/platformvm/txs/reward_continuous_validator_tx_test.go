// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

func TestRewardContinuousValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)

	type test struct {
		name   string
		txFunc func(*gomock.Controller) *RewardContinuousValidatorTx
		err    error
	}

	ctx := &snow.Context{
		ChainID:   ids.GenerateTestID(),
		NetworkID: uint32(1337),
	}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *RewardContinuousValidatorTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "missing timestamp",
			txFunc: func(*gomock.Controller) *RewardContinuousValidatorTx {
				return &RewardContinuousValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 0,
				}
			},
			err: errMissingTimestamp,
		},
		{
			name: "missing tx id",
			txFunc: func(*gomock.Controller) *RewardContinuousValidatorTx {
				return &RewardContinuousValidatorTx{
					Timestamp: 1,
				}
			},
			err: errMissingTxID,
		},
		{
			name: "valid continuous validator",
			txFunc: func(*gomock.Controller) *RewardContinuousValidatorTx {
				return &RewardContinuousValidatorTx{
					TxID:      ids.GenerateTestID(),
					Timestamp: 1,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(err, tt.err)
		})
	}
}

func TestAddContinuousValidatorIsUnsignedTx(t *testing.T) {
	require := require.New(t)

	txIntf := any((*RewardContinuousValidatorTx)(nil))
	_, ok := txIntf.(UnsignedTx)
	require.True(ok)
}
