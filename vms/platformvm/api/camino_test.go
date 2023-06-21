// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestBuildCaminoGenesis(t *testing.T) {
	hrp := constants.NetworkIDToHRP[testNetworkID]
	nodeID := ids.NodeID{1}
	addr := ids.ShortID(nodeID)
	addrStr, err := address.FormatBech32(hrp, addr.Bytes())
	require.NoError(t, err)

	avaxAssetID := ids.ID{}

	depositOffer := &deposit.Offer{
		InterestRateNominator:   1,
		Start:                   2,
		End:                     3,
		MinAmount:               4,
		MinDuration:             5,
		MaxDuration:             6,
		UnlockPeriodDuration:    7,
		NoRewardsPeriodDuration: 8,
		Flags:                   9,
		Memo:                    []byte("some memo"),
	}
	require.NoError(t, genesis.SetDepositOfferID(depositOffer))

	weight := json.Uint64(987654321)

	tests := map[string]struct {
		args            BuildGenesisArgs
		reply           BuildGenesisReply
		expectedReply   func() BuildGenesisReply
		expectedGenesis func(t *testing.T) (*genesis.Genesis, error)
		expectedErr     error
	}{
		"Happy Path": {
			args: BuildGenesisArgs{
				AvaxAssetID: avaxAssetID,
				NetworkID:   0,
				UTXOs: []UTXO{{
					Address: addrStr,
					Amount:  10,
				}},
				Validators: []PermissionlessValidator{{
					Staker: Staker{
						StartTime: 0,
						EndTime:   20,
						NodeID:    nodeID,
					},
					RewardOwner: &Owner{
						Threshold: 1,
						Addresses: []string{addrStr},
					},
					Staked: []UTXO{{
						Amount:  weight,
						Address: addrStr,
					}},
				}},
				Chains: []Chain{},
				Camino: Camino{
					VerifyNodeSignature:        true,
					LockModeBondDeposit:        true,
					ValidatorConsortiumMembers: []ids.ShortID{addr},
					ValidatorDeposits: [][]UTXODeposit{{{
						depositOffer.ID,
						10,
						10,
						"",
					}}},
					UTXODeposits: []UTXODeposit{{
						depositOffer.ID,
						10,
						10,
						"",
					}},
					DepositOffers: []*deposit.Offer{depositOffer},
					MultisigAliases: []*multisig.Alias{{
						ID:   addr,
						Memo: []byte("some memo"),
						Owners: &secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					}},
				},
				Time:          5,
				InitialSupply: 0,
				Message:       "",
				Encoding:      formatting.Hex,
			},
			reply: BuildGenesisReply{},
			expectedGenesis: func(t *testing.T) (*genesis.Genesis, error) {
				validatorTx := &txs.Tx{
					Unsigned: &txs.CaminoAddValidatorTx{
						AddValidatorTx: txs.AddValidatorTx{
							BaseTx: txs.BaseTx{
								BaseTx: avax.BaseTx{
									NetworkID:    0,
									BlockchainID: ids.Empty,
									Memo:         []byte{},
									Ins:          []*avax.TransferableInput{},
									Outs: []*avax.TransferableOutput{{
										Asset: avax.Asset{ID: avaxAssetID},
										Out: &locked.Out{
											IDs: locked.IDs{
												DepositTxID: ids.Empty,
												BondTxID:    locked.ThisTxID,
											},
											TransferableOut: &secp256k1fx.TransferOutput{
												Amt: uint64(weight),
												OutputOwners: secp256k1fx.OutputOwners{
													Threshold: 1,
													Addrs:     []ids.ShortID{addr},
												},
											},
										},
									}},
								},
							},
							Validator: txs.Validator{
								NodeID: nodeID,
								Start:  0,
								End:    20,
								Wght:   uint64(weight),
							},
							RewardsOwner: &secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
							StakeOuts: []*avax.TransferableOutput{},
						},
						NodeOwnerAuth: &secp256k1fx.Input{},
					},
					Creds: []verify.Verifiable{},
				}
				require.NoError(t, validatorTx.Sign(txs.GenesisCodec, nil))

				validatorDepositTx := &txs.Tx{
					Unsigned: &txs.DepositTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								NetworkID:    0,
								BlockchainID: ids.Empty,
								Memo:         []byte{},
								Ins: []*avax.TransferableInput{{
									UTXOID: avax.UTXOID{
										TxID:        validatorTx.ID(),
										OutputIndex: 0,
									},
									Asset: avax.Asset{ID: avaxAssetID},
									In: &locked.In{
										IDs: locked.IDs{
											BondTxID:    validatorTx.ID(),
											DepositTxID: ids.Empty,
										},
										TransferableIn: &secp256k1fx.TransferInput{
											Amt:   uint64(weight),
											Input: secp256k1fx.Input{SigIndices: []uint32{}},
										},
									},
								}},
								Outs: []*avax.TransferableOutput{{
									Asset: avax.Asset{ID: avaxAssetID},
									Out: &locked.Out{
										IDs: locked.IDs{
											BondTxID:    validatorTx.ID(),
											DepositTxID: locked.ThisTxID,
										},
										TransferableOut: &secp256k1fx.TransferOutput{
											Amt: uint64(weight),
											OutputOwners: secp256k1fx.OutputOwners{
												Threshold: 1,
												Addrs:     []ids.ShortID{addr},
											},
										},
									},
								}},
							},
						},
						DepositOfferID:  depositOffer.ID,
						DepositDuration: 10,
						RewardsOwner: &secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
					Creds: []verify.Verifiable{},
				}
				require.NoError(t, validatorDepositTx.Sign(txs.GenesisCodec, nil))

				depositTx := &txs.Tx{
					Unsigned: &txs.DepositTx{
						BaseTx: txs.BaseTx{
							BaseTx: avax.BaseTx{
								NetworkID:    0,
								BlockchainID: ids.Empty,
								Memo:         []byte{},
								Ins:          []*avax.TransferableInput{},
								Outs: []*avax.TransferableOutput{{
									Asset: avax.Asset{ID: avaxAssetID},
									Out: &locked.Out{
										IDs: locked.IDs{
											BondTxID:    ids.Empty,
											DepositTxID: locked.ThisTxID,
										},
										TransferableOut: &secp256k1fx.TransferOutput{
											Amt: 10,
											OutputOwners: secp256k1fx.OutputOwners{
												Threshold: 1,
												Addrs:     []ids.ShortID{addr},
											},
										},
									},
								}},
							},
						},
						DepositOfferID:  depositOffer.ID,
						DepositDuration: 10,
						RewardsOwner: &secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
					Creds: []verify.Verifiable{},
				}
				require.NoError(t, depositTx.Sign(txs.GenesisCodec, nil))

				return &genesis.Genesis{
					Timestamp:     5,
					InitialSupply: 0,
					Message:       "",
					Validators:    []*txs.Tx{},
					UTXOs: []*genesis.UTXO{
						{
							UTXO: avax.UTXO{
								UTXOID: avax.UTXOID{
									TxID:        validatorDepositTx.ID(),
									OutputIndex: 0,
								},
								Asset: avax.Asset{ID: avaxAssetID},
								Out: &locked.Out{
									IDs: locked.IDs{
										DepositTxID: validatorDepositTx.ID(),
										BondTxID:    validatorTx.ID(),
									},
									TransferableOut: &secp256k1fx.TransferOutput{
										Amt: uint64(weight),
										OutputOwners: secp256k1fx.OutputOwners{
											Locktime:  0,
											Threshold: 1,
											Addrs:     []ids.ShortID{addr},
										},
									},
								},
							},
							Message: []byte(""),
						},
						{
							UTXO: avax.UTXO{
								UTXOID: avax.UTXOID{
									TxID:        depositTx.ID(),
									OutputIndex: 0,
								},
								Asset: avax.Asset{ID: avaxAssetID},
								Out: &locked.Out{
									IDs: locked.IDs{
										DepositTxID: depositTx.ID(),
										BondTxID:    ids.Empty,
									},
									TransferableOut: &secp256k1fx.TransferOutput{
										Amt: 10,
										OutputOwners: secp256k1fx.OutputOwners{
											Locktime:  0,
											Threshold: 1,
											Addrs:     []ids.ShortID{addr},
										},
									},
								},
							},
							Message: []byte(""),
						},
					},
					Chains: []*txs.Tx{},
					Camino: genesis.Camino{
						VerifyNodeSignature: true,
						LockModeBondDeposit: true,
						InitialAdmin:        ids.ShortEmpty,
						AddressStates:       []genesis.AddressState{},
						DepositOffers:       []*deposit.Offer{depositOffer},
						Blocks: []*genesis.Block{
							{
								Timestamp:  0,
								Deposits:   []*txs.Tx{},
								Validators: []*txs.Tx{validatorTx},
							},
							{
								Timestamp:  15,
								Deposits:   []*txs.Tx{validatorDepositTx, depositTx},
								Validators: []*txs.Tx{},
							},
						},
						ConsortiumMembersNodeIDs: []genesis.ConsortiumMemberNodeID{{
							ConsortiumMemberAddress: addr,
							NodeID:                  nodeID,
						}},
						MultisigAliases: []*multisig.Alias{{
							ID:   addr,
							Memo: []byte("some memo"),
							Owners: &secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						}},
					},
				}, nil
			},
			expectedErr: nil,
		},
		"Wrong Lock Mode": {
			args: BuildGenesisArgs{
				AvaxAssetID: ids.ID{},
				NetworkID:   0,
				UTXOs: []UTXO{
					{
						Address: addrStr,
						Amount:  10,
					},
				},
				Validators: []PermissionlessValidator{
					{
						Staker: Staker{
							StartTime: 0,
							EndTime:   20,
							NodeID:    nodeID,
						},
						RewardOwner: &Owner{
							Threshold: 1,
							Addresses: []string{addrStr},
						},
						Staked: []UTXO{{
							Amount:  weight,
							Address: addrStr,
						}},
					},
				},
				Chains: []Chain{},
				Camino: Camino{
					VerifyNodeSignature: true,
					LockModeBondDeposit: false,
					ValidatorConsortiumMembers: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
					ValidatorDeposits: [][]UTXODeposit{
						{
							{
								depositOffer.ID,
								10,
								10,
								"",
							},
						},
					},
					UTXODeposits: []UTXODeposit{
						{
							depositOffer.ID,
							10,
							10,
							"",
						},
					},
					DepositOffers: []*deposit.Offer{depositOffer},
				},
				Time:          5,
				InitialSupply: 0,
				Message:       "",
				Encoding:      formatting.Hex,
			},
			reply:       BuildGenesisReply{},
			expectedErr: errWrongLockMode,
		},
		"Wrong UTXO Number": {
			args: BuildGenesisArgs{
				UTXOs: []UTXO{
					{
						Address: addrStr,
						Amount:  0,
					},
				},
				Validators: []PermissionlessValidator{},
				Time:       5,
				Encoding:   formatting.Hex,
				Camino: Camino{
					VerifyNodeSignature: true,
					LockModeBondDeposit: true,
					UTXODeposits: []UTXODeposit{
						{
							ids.GenerateTestID(),
							10,
							10,
							"",
						},
						{
							ids.GenerateTestID(),
							10,
							10,
							"",
						},
					},
				},
			},
			reply:       BuildGenesisReply{},
			expectedErr: errWrongUTXONumber,
		},
		"Wrong Validator Number": {
			args: BuildGenesisArgs{
				UTXOs: []UTXO{},
				Validators: []PermissionlessValidator{
					{
						Staker: Staker{
							StartTime: 0,
							EndTime:   20,
							NodeID:    nodeID,
						},
						RewardOwner: &Owner{
							Threshold: 1,
							Addresses: []string{addrStr},
						},
						Staked: []UTXO{{
							Amount:  weight,
							Address: addrStr,
						}},
					},
				},
				Time:     5,
				Encoding: formatting.Hex,
				Camino: Camino{
					VerifyNodeSignature: true,
					LockModeBondDeposit: true,
					ValidatorDeposits: [][]UTXODeposit{
						{
							{
								ids.GenerateTestID(),
								10,
								10,
								"",
							},
						},
						{
							{
								ids.GenerateTestID(),
								10,
								10,
								"",
							},
						},
					},
				},
			},
			reply:       BuildGenesisReply{},
			expectedErr: errWrongValidatorNumber,
		},
		"Deposits and Staked Misalignment": {
			args: BuildGenesisArgs{
				UTXOs: []UTXO{},
				Validators: []PermissionlessValidator{
					{
						Staker: Staker{
							StartTime: 0,
							EndTime:   20,
							NodeID:    nodeID,
						},
						RewardOwner: &Owner{
							Threshold: 1,
							Addresses: []string{addrStr},
						},
						Staked: []UTXO{},
					},
				},
				Time:     5,
				Encoding: formatting.Hex,
				Camino: Camino{
					VerifyNodeSignature: true,
					LockModeBondDeposit: true,
					ValidatorConsortiumMembers: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
					ValidatorDeposits: [][]UTXODeposit{
						{
							{
								ids.GenerateTestID(),
								10,
								10,
								"",
							},
						},
					},
				},
			},
			reply:       BuildGenesisReply{},
			expectedErr: errWrongDepositsAndStakedNumber,
		},
		"UTXO Has No Value": {
			args: BuildGenesisArgs{
				AvaxAssetID: ids.ID{},
				NetworkID:   0,
				UTXOs: []UTXO{
					{
						Address: addrStr,
						Amount:  0,
					},
				},
				Validators: []PermissionlessValidator{
					{
						Staker: Staker{
							StartTime: 0,
							EndTime:   20,
							NodeID:    nodeID,
						},
						RewardOwner: &Owner{
							Threshold: 1,
							Addresses: []string{addrStr},
						},
						Staked: []UTXO{{
							Amount:  weight,
							Address: addrStr,
						}},
					},
				},
				Chains: []Chain{},
				Camino: Camino{
					VerifyNodeSignature: true,
					LockModeBondDeposit: true,
					ValidatorConsortiumMembers: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
					ValidatorDeposits: [][]UTXODeposit{
						{
							{
								depositOffer.ID,
								10,
								10,
								"",
							},
						},
					},
					UTXODeposits: []UTXODeposit{
						{
							depositOffer.ID,
							10,
							10,
							"",
						},
					},
					DepositOffers: []*deposit.Offer{depositOffer},
				},
				Time:          5,
				InitialSupply: 0,
				Message:       "",
				Encoding:      formatting.Hex,
			},
			reply:       BuildGenesisReply{},
			expectedErr: errUTXOHasNoValue,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			actualReply := &BuildGenesisReply{}

			err := buildCaminoGenesis(&tt.args, actualReply)
			require.ErrorIs(t, err, tt.expectedErr)

			if tt.expectedGenesis != nil {
				expectedReply := &BuildGenesisReply{
					Encoding: formatting.Hex,
				}

				expectedGenesis, err := tt.expectedGenesis(t)
				require.NoError(t, err)

				bytes, err := genesis.Codec.Marshal(genesis.Version, expectedGenesis)
				require.NoError(t, err)

				expectedReply.Bytes = string(bytes)

				expectedReply.Bytes, err = formatting.Encode(expectedReply.Encoding, bytes)
				require.NoError(t, err)

				require.Equal(t, expectedReply, actualReply)
			}
		})
	}
}
