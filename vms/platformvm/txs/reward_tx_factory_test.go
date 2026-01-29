// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewRewardTxForStaker(t *testing.T) {
	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:   chainID,
		NetworkID: networkID,
	}

	validBaseTx := BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	blsSK, err := localsigner.New()
	require.NoError(t, err)

	blsPOP, err := signer.NewProofOfPossession(blsSK)
	require.NoError(t, err)

	tests := []struct {
		name           string
		stakerTxFunc   func(require *require.Assertions) *Tx
		timestamp      time.Time
		expectedTxType any
	}{
		{
			name: "continuous staker returns RewardContinuousValidatorTx",
			stakerTxFunc: func(require *require.Assertions) *Tx {
				utx := &AddContinuousValidatorTx{
					BaseTx:                validBaseTx,
					ValidatorNodeID:       ids.GenerateTestNodeID(),
					Period:                1,
					Wght:                  2,
					Signer:                blsPOP,
					StakeOuts:             []*avax.TransferableOutput{},
					ValidatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegationShares:      reward.PercentDenominator,
					ConfigOwner:           &secp256k1fx.OutputOwners{},
				}

				tx, err := NewSigned(utx, Codec, nil)
				require.NoError(err)
				return tx
			},
			timestamp:      time.Unix(1000, 0),
			expectedTxType: &RewardContinuousValidatorTx{},
		},
		{
			name: "fixed staker returns RewardValidatorTx",
			stakerTxFunc: func(require *require.Assertions) *Tx {
				utx := &AddPermissionlessValidatorTx{
					BaseTx: validBaseTx,
					Validator: Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(time.Now().Add(time.Hour).Unix()),
						Wght:   2,
					},
					Subnet:                ids.GenerateTestID(),
					Signer:                blsPOP,
					StakeOuts:             []*avax.TransferableOutput{},
					ValidatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegationShares:      reward.PercentDenominator,
				}

				tx, err := NewSigned(utx, Codec, nil)
				require.NoError(err)
				return tx
			},
			timestamp:      time.Unix(1000, 0),
			expectedTxType: &RewardValidatorTx{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			stakerTx := tt.stakerTxFunc(require)

			rewardTx, err := NewRewardTxForStaker(ctx, stakerTx, tt.timestamp)
			require.NoError(err)
			require.NotNil(rewardTx)
			require.IsType(tt.expectedTxType, rewardTx.Unsigned)

			switch utx := rewardTx.Unsigned.(type) {
			case *RewardContinuousValidatorTx:
				require.Equal(stakerTx.ID(), utx.TxID)
				require.Equal(uint64(tt.timestamp.Unix()), utx.Timestamp)
			case *RewardValidatorTx:
				require.Equal(stakerTx.ID(), utx.TxID)
			}
		})
	}
}
