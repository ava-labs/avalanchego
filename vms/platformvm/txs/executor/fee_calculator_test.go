// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
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
	inputs, outputs, stakes, _ := txsCreationHelpers(defaultCtx, validatorWeight)
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

func TestAddSubnetValidatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddSubnetValidatorTx
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
	subnetID := ids.GenerateTestID()
	inputs, outputs, _, subnetAuth := txsCreationHelpers(defaultCtx, validatorWeight)
	uTx := &txs.AddSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		SubnetValidator: txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: defaultCtx.NodeID,
				Start:  uint64(time.Now().Truncate(time.Second).Unix()),
				End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
				Wght:   validatorWeight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
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
				require.Equal(t, 3355*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestAddDelegatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddDelegatorTx
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
	inputs, outputs, stakes, _ := txsCreationHelpers(defaultCtx, validatorWeight)
	uTx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Outs:         outputs,
			Ins:          inputs,
			Memo:         []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		Validator: txs.Validator{
			NodeID: defaultCtx.NodeID,
			Start:  uint64(time.Now().Truncate(time.Second).Unix()),
			End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
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
				require.Equal(t, 3725*units.MicroAvax, fc.Fee)
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
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestCreateChainTxFees(t *testing.T) {
	// For simplicity, we define a single CreateChainTx
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
	inputs, outputs, _, subnetAuth := txsCreationHelpers(defaultCtx, 0)
	uTx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		SubnetID:    ids.GenerateTestID(),
		ChainName:   "testingStuff",
		VMID:        ids.GenerateTestID(),
		FxIDs:       []ids.ID{ids.GenerateTestID()},
		GenesisData: []byte{0xff},
		SubnetAuth:  subnetAuth,
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
				require.Equal(t, 3390*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestCreateSubnetTxFees(t *testing.T) {
	// For simplicity, we define a single CreateSubnetTx
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
	inputs, outputs, _, _ := txsCreationHelpers(defaultCtx, 0)
	uTx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Owner: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
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
				require.Equal(t, 3295*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestRemoveSubnetValidatorTxFees(t *testing.T) {
	// For simplicity, we define a single RemoveSubnetValidatorTx
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
	inputs, outputs, _, auth := txsCreationHelpers(defaultCtx, 0)
	uTx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		NodeID:     ids.GenerateTestNodeID(),
		Subnet:     ids.GenerateTestID(),
		SubnetAuth: auth,
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
				require.Equal(t, 3323*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestTransformSubnetTxFees(t *testing.T) {
	// For simplicity, we define a single TransformSubnetTx
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
	inputs, outputs, _, auth := txsCreationHelpers(defaultCtx, 0)
	uTx := &txs.TransformSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Subnet:                   ids.GenerateTestID(),
		AssetID:                  ids.GenerateTestID(),
		InitialSupply:            0x1000000000000000,
		MaximumSupply:            0x1000000000000000,
		MinConsumptionRate:       0,
		MaxConsumptionRate:       0,
		MinValidatorStake:        1,
		MaxValidatorStake:        0x1000000000000000,
		MinStakeDuration:         1,
		MaxStakeDuration:         1,
		MinDelegationFee:         0,
		MinDelegatorStake:        0xffffffffffffffff,
		MaxValidatorWeightFactor: 255,
		UptimeRequirement:        0,
		SubnetAuth:               auth,
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
				require.Equal(t, 3408*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestTransferSubnetOwnershipTxFees(t *testing.T) {
	// For simplicity, we define a single TransferSubnetOwnershipTx
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
	inputs, outputs, _, _ := txsCreationHelpers(defaultCtx, 0)
	uTx := &txs.TransferSubnetOwnershipTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Subnet: ids.GenerateTestID(),
		SubnetAuth: &secp256k1fx.Input{
			SigIndices: []uint32{3},
		},
		Owner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
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
				require.Equal(t, 3339*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestAddPermissionlessValidatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddPermissionlessValidatorTx
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
	inputs, outputs, stakes, _ := txsCreationHelpers(defaultCtx, 0)
	sk, err := bls.NewSecretKey()
	r.NoError(err)
	uTx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Subnet:    ids.GenerateTestID(),
		Signer:    signer.NewProofOfPossession(sk),
		StakeOuts: stakes,
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
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
				require.Equal(t, 3941*units.MicroAvax, fc.Fee)
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
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestAddPermissionlessDelegatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddPermissionlessDelegatorTx
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
	inputs, outputs, stakes, _ := txsCreationHelpers(defaultCtx, 2*units.KiloAvax)
	uTx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Validator: txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  12345,
			End:    12345 + 200*24*60*60,
			Wght:   2 * units.KiloAvax,
		},
		Subnet:    ids.GenerateTestID(),
		StakeOuts: stakes,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
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
				require.Equal(t, 3749*units.MicroAvax, fc.Fee)
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
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestBaseTxFees(t *testing.T) {
	// For simplicity, we define a single BaseTx
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
	inputs, outputs, _, _ := txsCreationHelpers(defaultCtx, 2*units.KiloAvax)
	uTx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    defaultCtx.NetworkID,
		BlockchainID: defaultCtx.ChainID,
		Ins:          inputs,
		Outs:         outputs,
	}}
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
				require.Equal(t, 3255*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						172,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestImportTxFees(t *testing.T) {
	// For simplicity, we define a single ImportTx
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
			3000,
			2500,
			1000,
			1000,
		},
		AddPrimaryNetworkValidatorFee: 0,
	}

	// create a well formed AddValidatorTx
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}
	inputs, outputs, _, _ := txsCreationHelpers(defaultCtx, 2*units.KiloAvax)
	uTx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		SourceChain: ids.GenerateTestID(),
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
			In: &secp256k1fx.TransferInput{
				Amt:   50000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
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
				require.Equal(t, 5829*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						2180,                     // this is more complex rule, gotta check against hard-coded value
						262,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func TestExportTxFees(t *testing.T) {
	// For simplicity, we define a single ExportTx
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
			3000,
			2500,
			1000,
			1000,
		},
		AddPrimaryNetworkValidatorFee: 0,
	}

	// create a well formed AddValidatorTx
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}
	inputs, outputs, _, _ := txsCreationHelpers(defaultCtx, 2*units.KiloAvax)
	uTx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		DestinationChain: ids.GenerateTestID(),
		ExportedOutputs:  outputs,
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
				require.Equal(t, 3617*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())), // this is a simple rule, can assert easily
						1090,                     // this is more complex rule, gotta check against hard-coded value
						254,                      // this is more complex rule, gotta check against hard-coded value
						0,
					},
					fc.feeManager.GetCumulatedUnits(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   defaultFeeCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}
				cfg.DefaultBlockMaxConsumedUnits[1] = 1090 - 1

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

func txsCreationHelpers(defaultCtx *snow.Context, stakeWeight uint64) (
	inputs []*avax.TransferableInput,
	outputs []*avax.TransferableOutput,
	stakes []*avax.TransferableOutput,
	auth *secp256k1fx.Input,
) {
	inputs = []*avax.TransferableInput{{
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
	outputs = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
			},
		},
	}}
	stakes = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(time.Now().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: stakeWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
				},
			},
		},
	}}
	auth = &secp256k1fx.Input{
		SigIndices: []uint32{0, 1},
	}
	return inputs, outputs, stakes, auth
}
