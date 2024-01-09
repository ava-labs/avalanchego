// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddValidatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddValidatorTx
	// and we change config parameters in the different test cases
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	defaultFeeCfg := config.FeeConfig{
		DefaultUnitFees: fees.Dimensions{
			1 * units.MicroAvax,
			2 * units.MicroAvax,
			3 * units.MicroAvax,
			4 * units.MicroAvax,
		},
		DefaultBlockMaxConsumedUnits: fees.Dimensions{
			1000,
			1500,
			1000,
			1000,
		},
		AddPrimaryNetworkValidatorFee: 0,
	}

	// create a well formed AddValidatorTx
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}
	validatorWeight := uint64(2024)
	inputs := []*avax.TransferableInput{{
		UTXOID: avax.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
			},
		},
	}}
	stakes := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(time.Now().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: validatorWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}}
	uTx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Validator: txs.Validator{
			NodeID: defaultCtx.NodeID,
			Start:  uint64(time.Now().Truncate(time.Second).Unix()),
			End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		DelegationShares: reward.PercentDenominator,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, signers)
	r.NoError(err)

	type test struct {
		description      string
		cfgAndChainTimeF func() (*config.Config, time.Time)
		expectedError    error
		checksF          func(*testing.T, *FeeCalculator)
	}

	tests := []test{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, 3721*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						266,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, bandwidth cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[0] = uint64(len(stx.Bytes())) - 1

				return cfg, chainTime
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(t *testing.T, fc *FeeCalculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			fc := &FeeCalculator{
				feeManager: fees.NewManager(cfg.DefaultUnitFees),
				Config:     cfg,
				ChainTime:  chainTime,
				Tx:         stx,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}
