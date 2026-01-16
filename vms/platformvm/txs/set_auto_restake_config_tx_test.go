// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

func TestSetAutoRestakeConfigTxSyntacticVerify(t *testing.T) {
	require := require.New(t)

	type test struct {
		name   string
		txFunc func(*gomock.Controller) *SetAutoRestakeConfigTx
		err    error
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

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				return &SetAutoRestakeConfigTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "empty txID",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				return &SetAutoRestakeConfigTx{
					BaseTx: validBaseTx,
				}
			},
			err: errMissingTxID,
		},
		{
			name: "too many restake shares",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				autoRestakeShares := uint32(2_000_000)

				return &SetAutoRestakeConfigTx{
					BaseTx:               validBaseTx,
					TxID:                 ids.GenerateTestID(),
					AutoRestakeShares:    autoRestakeShares,
					HasAutoRestakeShares: true,
				}
			},
			err: errTooManyRestakeShares,
		},
		{
			name: "err no updated fields",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				return &SetAutoRestakeConfigTx{
					BaseTx: validBaseTx,
					TxID:   ids.GenerateTestID(),
				}
			},
			err: errNoUpdatedFields,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *SetAutoRestakeConfigTx {
				autoRestakeShares := uint32(500_000)

				return &SetAutoRestakeConfigTx{
					BaseTx:               invalidBaseTx,
					TxID:                 ids.GenerateTestID(),
					AutoRestakeShares:    autoRestakeShares,
					HasAutoRestakeShares: true,
				}
			},
			err: avax.ErrWrongNetworkID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(ctx)
			require.ErrorIs(err, tt.err)

			if tx != nil {
				require.Equal(tt.err == nil, tx.SyntacticallyVerified)
			}
		})
	}
}
