// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/hex"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestAddAutoRenewedValidatorTxSyntacticVerify(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx
		want   error
	}{
		{
			name: "nil",
			mutate: func(*AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				return nil
			},
			want: ErrNilTx,
		},
		{
			name: "already_verified",
			mutate: func(*AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				return &AddAutoRenewedValidatorTx{
					BaseTx: BaseTx{
						SyntacticallyVerified: true,
					},
				}
			},
			want: nil,
		},
		{
			name: "empty_node_id",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.ValidatorNodeID = ids.EmptyNodeID.Bytes()
				return tx
			},
			want: errEmptyNodeID,
		},
		{
			name: "unsupported_node_id_length",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.ValidatorNodeID = make([]byte, 32)
				return tx
			},
			want: hashing.ErrInvalidHashLen,
		},
		{
			name: "no_stake",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.StakeOuts = nil
				return tx
			},
			want: errNoStake,
		},
		{
			name: "missing_period",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.Period = 0
				return tx
			},
			want: errMissingPeriod,
		},
		{
			name: "too_many_shares",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.DelegationShares = reward.PercentDenominator + 1
				return tx
			},
			want: errTooManyShares,
		},
		{
			name: "too_many_auto_compound_reward_shares",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.AutoCompoundRewardShares = reward.PercentDenominator + 1
				return tx
			},
			want: errTooManyAutoCompoundRewardShares,
		},
		{
			name: "invalid_BaseTx",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.BaseTx = BaseTx{}
				return tx
			},
			want: avax.ErrWrongNetworkID,
		},
		{
			name: "invalid_validator_rewards_owner",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.ValidatorRewardsOwner = &secp256k1fx.OutputOwners{
					Threshold: 1,
				}
				return tx
			},
			want: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "invalid_delegator_rewards_owner",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.DelegatorRewardsOwner = &secp256k1fx.OutputOwners{
					Threshold: 1,
				}
				return tx
			},
			want: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "invalid_owner",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.ValidatorAuthority = &secp256k1fx.OutputOwners{
					Threshold: 1,
				}
				return tx
			},
			want: secp256k1fx.ErrOutputUnspendable,
		},
		{
			name: "wrong_signer",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.Signer = &signer.Empty{}
				return tx
			},
			want: errMissingSigner,
		},
		{
			name: "invalid_stake_output",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.StakeOuts[0].Out = nil // triggers ErrNilTransferableFxOutput
				return tx
			},
			want: avax.ErrNilTransferableFxOutput,
		},
		{
			name: "stake_overflow",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.StakeOuts[0].Out = &secp256k1fx.TransferOutput{
					Amt: math.MaxUint64,
				}
				return tx
			},
			want: safemath.ErrOverflow,
		},
		{
			name: "invalid_staked_asset",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.StakeOuts[0].Asset.ID = ids.GenerateTestID()
				return tx
			},
			want: errInvalidStakedAsset,
		},
		{
			name: "stake_not_sorted",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				tx.StakeOuts[0].Out = &secp256k1fx.TransferOutput{
					Amt: 2,
				}
				return tx
			},
			want: errOutputsNotSorted,
		},
		{
			name: "valid",
			mutate: func(tx *AddAutoRenewedValidatorTx) *AddAutoRenewedValidatorTx {
				return tx
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := snowtest.Context(t, snowtest.PChainID)

			blsSK, err := localsigner.New()
			require.NoError(t, err)

			blsPOP, err := signer.NewProofOfPossession(blsSK)
			require.NoError(t, err)

			tx := tt.mutate(&AddAutoRenewedValidatorTx{
				BaseTx: BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    ctx.NetworkID,
						BlockchainID: ctx.ChainID,
					},
				},
				ValidatorNodeID: ids.GenerateTestNodeID().Bytes(),
				Period:          1,
				Signer:          blsPOP,
				StakeOuts: []*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
					{
						Asset: avax.Asset{
							ID: ctx.AVAXAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
				ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
				DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
				ValidatorAuthority: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
				DelegationShares: reward.PercentDenominator,
			})

			got := tx.SyntacticVerify(ctx)
			require.ErrorIs(t, got, tt.want)

			if tx != nil {
				require.Equal(t, tt.want == nil, tx.SyntacticallyVerified)
			}
		})
	}
}

type testOwner struct {
	secp256k1fx.OutputOwners
	initCtxCalled bool
}

func (o *testOwner) InitCtx(*snow.Context) {
	o.initCtxCalled = true
}

type testTransferableOut struct {
	secp256k1fx.TransferOutput
	initCtxCalled bool
}

func (o *testTransferableOut) InitCtx(*snow.Context) {
	o.initCtxCalled = true
}

func TestAddAutoRenewedValidatorTxInitCtx(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.PChainID)

	baseIn := &avax.TransferableInput{
		Asset: avax.Asset{ID: ids.GenerateTestID()},
	}
	baseOut := &avax.TransferableOutput{
		Asset: avax.Asset{ID: ids.GenerateTestID()},
		Out:   &secp256k1fx.TransferOutput{Amt: 1},
	}
	stakeInnerOut := &testTransferableOut{
		TransferOutput: secp256k1fx.TransferOutput{Amt: 1},
	}
	stakeOut := &avax.TransferableOutput{
		Asset: avax.Asset{ID: ids.GenerateTestID()},
		Out:   stakeInnerOut,
	}

	validatorRewardsOwner := &testOwner{}
	delegatorRewardsOwner := &testOwner{}
	owner := &testOwner{}

	tx := &AddAutoRenewedValidatorTx{
		BaseTx: BaseTx{
			BaseTx: avax.BaseTx{
				Ins:  []*avax.TransferableInput{baseIn},
				Outs: []*avax.TransferableOutput{baseOut},
			},
		},
		StakeOuts:             []*avax.TransferableOutput{stakeOut},
		ValidatorRewardsOwner: validatorRewardsOwner,
		DelegatorRewardsOwner: delegatorRewardsOwner,
		ValidatorAuthority:    owner,
	}

	tx.InitCtx(ctx)

	require.Equal(secp256k1fx.ID, baseIn.FxID)
	require.Equal(secp256k1fx.ID, baseOut.FxID)
	require.Equal(secp256k1fx.ID, stakeOut.FxID)
	require.True(stakeInnerOut.initCtxCalled)
	require.True(validatorRewardsOwner.initCtxCalled)
	require.True(delegatorRewardsOwner.initCtxCalled)
	require.True(owner.initCtxCalled)
}

func TestAddAutoRenewedValidatorTxSerialization(t *testing.T) {
	require := require.New(t)

	addr := ids.ShortID{
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
	}

	skBytes, err := hex.DecodeString("6668fecd4595b81e4d568398c820bbf3f073cb222902279fa55ebb84764ed2e3")
	require.NoError(err)

	sk, err := localsigner.FromBytes(skBytes)
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	avaxAssetID, err := ids.FromString("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	require.NoError(err)

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}
	nodeID := ids.BuildTestNodeID([]byte{
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x11, 0x22, 0x33, 0x44,
	})

	tx := &AddAutoRenewedValidatorTx{
		BaseTx: BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        txID,
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		ValidatorNodeID: types.JSONByteSlice(nodeID.Bytes()),
		Signer:          pop,
		StakeOuts: []*avax.TransferableOutput{
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
		},
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		},
		ValidatorAuthority: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				addr,
			},
		},
		DelegationShares:         reward.PercentDenominator,
		AutoCompoundRewardShares: 500_000,
		Period:                   200 * 24 * 60 * 60,
	}
	avax.SortTransferableOutputs(tx.Outs, Codec)
	avax.SortTransferableOutputs(tx.StakeOuts, Codec)
	utils.Sort(tx.Ins)
	require.NoError(tx.SyntacticVerify(&snow.Context{
		NetworkID:   1,
		ChainID:     constants.PlatformChainID,
		AVAXAssetID: avaxAssetID,
	}))

	wantBytes := []byte{
		// Codec version
		0x00, 0x00,
		// AddAutoRenewedValidatorTx type ID
		0x00, 0x00, 0x00, 0x28,
		// Mainnet network ID
		0x00, 0x00, 0x00, 0x01,
		// P-chain blockchain ID
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// Number of immediate outputs
		0x00, 0x00, 0x00, 0x00,
		// Number of inputs
		0x00, 0x00, 0x00, 0x01,
		// inputs[0]
		// TxID
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		// Tx output index
		0x00, 0x00, 0x00, 0x01,
		// Mainnet AVAX asset ID
		0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
		0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
		0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
		0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		// secp256k1fx transfer input type ID
		0x00, 0x00, 0x00, 0x05,
		// Amount = 2k AVAX
		0x00, 0x00, 0x01, 0xd1, 0xa9, 0x4a, 0x20, 0x00,
		// Number of input signature indices
		0x00, 0x00, 0x00, 0x01,
		// signature index
		0x00, 0x00, 0x00, 0x01,
		// memo length
		0x00, 0x00, 0x00, 0x00,
		// NodeID length
		0x00, 0x00, 0x00, 0x14,
		// NodeID
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x11, 0x22, 0x33, 0x44,
		// BLS PoP type ID
		0x00, 0x00, 0x00, 0x1c,
		// BLS compressed public key
		0xaf, 0xf4, 0xac, 0xb4, 0xc5, 0x43, 0x9b, 0x5d,
		0x42, 0x6c, 0xad, 0xf9, 0xe9, 0x46, 0xd3, 0xa4,
		0x52, 0xf7, 0xde, 0x34, 0x14, 0xd1, 0xad, 0x27,
		0x33, 0x61, 0x33, 0x21, 0x1d, 0x8b, 0x90, 0xcf,
		0x49, 0xfb, 0x97, 0xee, 0xbc, 0xde, 0xee, 0xf7,
		0x14, 0xdc, 0x20, 0xf5, 0x4e, 0xd0, 0xd4, 0xd1,
		// BLS compressed proof of possession
		0x8c, 0xfd, 0x79, 0x09, 0xd1, 0x53, 0xb9, 0x60,
		0x4b, 0x62, 0xb1, 0x43, 0xba, 0x36, 0x20, 0x7b,
		0xb7, 0xe6, 0x48, 0x67, 0x42, 0x44, 0x80, 0x20,
		0x2a, 0x67, 0xdc, 0x68, 0x76, 0x83, 0x46, 0xd9,
		0x5c, 0x90, 0x98, 0x3c, 0x2d, 0x27, 0x9c, 0x64,
		0xc4, 0x3c, 0x51, 0x13, 0x6b, 0x2a, 0x05, 0xe0,
		0x16, 0x02, 0xd5, 0x2a, 0xa6, 0x37, 0x6f, 0xda,
		0x17, 0xfa, 0x6e, 0x2a, 0x18, 0xa0, 0x83, 0xe4,
		0x9d, 0x9c, 0x45, 0x0e, 0xab, 0x7b, 0x89, 0xb1,
		0xd5, 0x55, 0x5d, 0xa5, 0xc4, 0x89, 0x87, 0x2e,
		0x02, 0xb7, 0xe5, 0x22, 0x7b, 0x77, 0x55, 0x0a,
		0xf1, 0x33, 0x0e, 0x5a, 0x71, 0xf8, 0xc3, 0x68,
		// Number of staked outputs
		0x00, 0x00, 0x00, 0x01,
		// Mainnet AVAX asset ID
		0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a,
		0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46, 0x27, 0x78,
		0xa8, 0xf5, 0x22, 0x74, 0xb9, 0xd6, 0x05, 0xdf,
		0x25, 0x91, 0xb2, 0x30, 0x27, 0xa8, 0x7d, 0xff,
		// secp256k1fx transferable output type ID
		0x00, 0x00, 0x00, 0x07,
		// amount = 2k AVAX
		0x00, 0x00, 0x01, 0xd1, 0xa9, 0x4a, 0x20, 0x00,
		// locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// addresses[0]
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
		// secp256k1fx owner type ID (validator rewards owner)
		0x00, 0x00, 0x00, 0x0b,
		// locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// addresses[0]
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
		// secp256k1fx owner type ID (delegator rewards owner)
		0x00, 0x00, 0x00, 0x0b,
		// locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// addresses[0]
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
		// secp256k1fx owner type ID (validator authority)
		0x00, 0x00, 0x00, 0x0b,
		// locktime
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// addresses[0]
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
		// delegation shares (1,000,000)
		0x00, 0x0f, 0x42, 0x40,
		// auto compound reward shares (500,000)
		0x00, 0x07, 0xa1, 0x20,
		// period = 200 days in seconds (17,280,000)
		0x00, 0x00, 0x00, 0x00, 0x01, 0x07, 0xac, 0x00,
	}

	var unsignedTx UnsignedTx = tx
	gotBytes, err := Codec.Marshal(CodecVersion, &unsignedTx)
	require.NoError(err)
	require.Equal(wantBytes, gotBytes)
}
