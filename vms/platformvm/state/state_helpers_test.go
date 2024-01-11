// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestGetStakerRewardAttributes(t *testing.T) {
	type test struct {
		name               string
		chainF             func(*gomock.Controller) Chain
		expectedAttributes *StakerRewardAttributes
		expectedErr        error
	}

	var (
		stakerID    = ids.GenerateTestID()
		shares      = uint32(2024)
		avaxAssetID = ids.GenerateTestID()
		addr        = ids.GenerateTestShortID()
		outputs     = []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							addr,
						},
					},
				},
			},
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &stakeable.LockOut{
					Locktime: 87654321,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  12345678,
							Threshold: 0,
							Addrs:     []ids.ShortID{},
						},
					},
				},
			},
		}
		stakeOutputs = []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: avaxAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							addr,
						},
					},
				},
			},
		}
		anOwner = &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		}
		anotherOwner = &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 2,
			Addrs: []ids.ShortID{
				addr,
			},
		}
	)
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pop := signer.NewProofOfPossession(sk)

	tests := []test{
		{
			name: "permissionless validator tx type",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				validatorTx := &txs.Tx{
					Unsigned: &txs.AddPermissionlessValidatorTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								Outs: outputs,
							},
						},
						StakeOuts:             stakeOutputs,
						ValidatorRewardsOwner: anOwner,
						DelegatorRewardsOwner: anotherOwner,
						DelegationShares:      shares,
						Signer:                pop,
					},
				}
				chain.EXPECT().GetTx(stakerID).Return(validatorTx, status.Committed, nil)
				return chain
			},
			expectedAttributes: &StakerRewardAttributes{
				Stake:                  stakeOutputs,
				Outputs:                outputs,
				Shares:                 shares,
				ValidationRewardsOwner: anOwner,
				DelegationRewardsOwner: anotherOwner,
				ProofOfPossession:      pop,
			},
			expectedErr: nil,
		},
		{
			name: "non permissionless validator tx type",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				validatorTx := &txs.Tx{
					Unsigned: &txs.AddValidatorTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								Outs: outputs,
							},
						},
						StakeOuts:        stakeOutputs,
						RewardsOwner:     anOwner,
						DelegationShares: shares,
					},
				}
				chain.EXPECT().GetTx(stakerID).Return(validatorTx, status.Committed, nil)
				return chain
			},
			expectedAttributes: &StakerRewardAttributes{
				Stake:                  stakeOutputs,
				Outputs:                outputs,
				Shares:                 shares,
				ValidationRewardsOwner: anOwner,
				DelegationRewardsOwner: anOwner,
				ProofOfPossession:      nil,
			},
			expectedErr: nil,
		},
		{
			name: "delegator tx type",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				delegatorTx := &txs.Tx{
					Unsigned: &txs.AddPermissionlessDelegatorTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								Outs: outputs,
							},
						},
						StakeOuts:              stakeOutputs,
						DelegationRewardsOwner: anOwner,
					},
				}
				chain.EXPECT().GetTx(stakerID).Return(delegatorTx, status.Committed, nil)
				return chain
			},
			expectedAttributes: &StakerRewardAttributes{
				Stake:        stakeOutputs,
				Outputs:      outputs,
				RewardsOwner: anOwner,
			},
			expectedErr: nil,
		},
		{
			name: "missing tx",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				chain.EXPECT().GetTx(stakerID).Return(nil, status.Unknown, database.ErrNotFound)
				return chain
			},
			expectedAttributes: nil,
			expectedErr:        database.ErrNotFound,
		},
		{
			name: "unexpected tx type",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				wrongTxType := &txs.Tx{
					Unsigned: &txs.CreateChainTx{},
				}
				chain.EXPECT().GetTx(stakerID).Return(wrongTxType, status.Committed, nil)
				return chain
			},
			expectedAttributes: nil,
			expectedErr:        ErrUnexpectedStakerType,
		},
		{
			name: "getStakerRewardAttributes works across layers",
			chainF: func(c *gomock.Controller) Chain {
				// pile a diff on top of base state and let the target tx
				// be included in base state.
				state := NewMockState(c)
				validatorTx := &txs.Tx{
					Unsigned: &txs.AddPermissionlessValidatorTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								Outs: outputs,
							},
						},
						StakeOuts:             stakeOutputs,
						ValidatorRewardsOwner: anOwner,
						DelegatorRewardsOwner: anotherOwner,
						DelegationShares:      shares,
						Signer:                pop,
					},
				}
				state.EXPECT().GetTx(stakerID).Return(validatorTx, status.Committed, nil)
				state.EXPECT().GetTimestamp().Return(time.Now()) // needed to build diff
				stateID := ids.GenerateTestID()

				versions := NewMockVersions(c)
				versions.EXPECT().GetState(stateID).Return(state, true).Times(2)

				diff, err := NewDiff(stateID, versions)
				require.NoError(t, err)
				return diff
			},
			expectedAttributes: &StakerRewardAttributes{
				Stake:                  stakeOutputs,
				Outputs:                outputs,
				Shares:                 shares,
				ValidationRewardsOwner: anOwner,
				DelegationRewardsOwner: anotherOwner,
				ProofOfPossession:      pop,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := tt.chainF(ctrl)
			attributes, err := getStakerRewardAttributes(chain, stakerID)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedAttributes, attributes)
		})
	}
}

func TestGetChainSubnet(t *testing.T) {
	type test struct {
		name             string
		chainF           func(*gomock.Controller) Chain
		expectedSubnetID ids.ID
		expectedErr      error
	}

	var (
		chainID  = ids.GenerateTestID()
		subnetID = ids.GenerateTestID()
	)

	tests := []test{
		{
			name: "subnet from existing chain",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				createChainTx := &txs.Tx{
					Unsigned: &txs.CreateChainTx{
						SubnetID: subnetID,
					},
					TxID: chainID,
				}
				chain.EXPECT().GetTx(chainID).Return(createChainTx, status.Committed, nil)
				return chain
			},
			expectedSubnetID: subnetID,
			expectedErr:      nil,
		},
		{
			name: "missing tx",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				chain.EXPECT().GetTx(chainID).Return(nil, status.Unknown, database.ErrNotFound)
				return chain
			},
			expectedSubnetID: ids.Empty,
			expectedErr:      database.ErrNotFound,
		},
		{
			name: "unexpected tx type",
			chainF: func(c *gomock.Controller) Chain {
				chain := NewMockChain(c)
				wrongTxType := &txs.Tx{
					Unsigned: &txs.CreateSubnetTx{},
				}
				chain.EXPECT().GetTx(chainID).Return(wrongTxType, status.Committed, nil)
				return chain
			},
			expectedSubnetID: ids.Empty,
			expectedErr:      errNotABlockchain,
		},
		{
			name: "getChainSubnet works across layers",
			chainF: func(c *gomock.Controller) Chain {
				// pile a diff on top of base state and let the target tx
				// be included in base state.
				state := NewMockState(c)
				createChainTx := &txs.Tx{
					Unsigned: &txs.CreateChainTx{
						SubnetID: subnetID,
					},
					TxID: chainID,
				}
				state.EXPECT().GetTx(chainID).Return(createChainTx, status.Committed, nil)
				state.EXPECT().GetTimestamp().Return(time.Now()) // needed to build diff
				stateID := ids.GenerateTestID()

				versions := NewMockVersions(c)
				versions.EXPECT().GetState(stateID).Return(state, true).Times(2)

				diff, err := NewDiff(stateID, versions)
				require.NoError(t, err)
				return diff
			},
			expectedSubnetID: subnetID,
			expectedErr:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := tt.chainF(ctrl)
			subnetID, err := getChainSubnet(chain, chainID)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedSubnetID, subnetID)
		})
	}
}
