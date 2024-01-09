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

var (
	feeTestsDefaultCfg = config.FeeConfig{
		DefaultUnitFees: fees.Dimensions{
			1 * units.MicroAvax,
			2 * units.MicroAvax,
			3 * units.MicroAvax,
			4 * units.MicroAvax,
		},
		DefaultBlockMaxConsumedUnits: fees.Dimensions{
			3000,
			3500,
			1000,
			1000,
		},

		TxFee:                         1 * units.Avax,
		CreateAssetTxFee:              2 * units.Avax,
		CreateSubnetTxFee:             3 * units.Avax,
		TransformSubnetTxFee:          4 * units.Avax,
		CreateBlockchainTxFee:         5 * units.Avax,
		AddPrimaryNetworkValidatorFee: 6 * units.Avax,
		AddPrimaryNetworkDelegatorFee: 7 * units.Avax,
		AddSubnetValidatorFee:         8 * units.Avax,
		AddSubnetDelegatorFee:         9 * units.Avax,
	}

	feeTestSigners            = [][]*secp256k1.PrivateKey{preFundedKeys}
	feeTestDefaultStakeWeight = uint64(2024)
)

type feeTests struct {
	description      string
	cfgAndChainTimeF func() (*config.Config, time.Time)
	expectedError    error
	checksF          func(*testing.T, *FeeCalculator)
}

func TestAddValidatorTxFees(t *testing.T) {
	// For simplicity, we define a single AddValidatorTx
	// and we change config parameters in the different test cases
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, stakes, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.AddValidatorTx{
		BaseTx: baseTx,
		Validator: txs.Validator{
			NodeID: defaultCtx.NodeID,
			Start:  uint64(time.Now().Truncate(time.Second).Unix()),
			End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
			Wght:   feeTestDefaultStakeWeight,
		},
		StakeOuts: stakes,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		DelegationShares: reward.PercentDenominator,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						266,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	subnetID := ids.GenerateTestID()
	baseTx, _, subnetAuth := txsCreationHelpers(defaultCtx)
	uTx := &txs.AddSubnetValidatorTx{
		BaseTx: baseTx,
		SubnetValidator: txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: defaultCtx.NodeID,
				Start:  uint64(time.Now().Truncate(time.Second).Unix()),
				End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
				Wght:   feeTestDefaultStakeWeight,
			},
			Subnet: subnetID,
		},
		SubnetAuth: subnetAuth,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, 3347*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, stakes, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.AddDelegatorTx{
		BaseTx: baseTx,
		Validator: txs.Validator{
			NodeID: defaultCtx.NodeID,
			Start:  uint64(time.Now().Truncate(time.Second).Unix()),
			End:    uint64(time.Now().Truncate(time.Second).Add(time.Hour).Unix()),
			Wght:   feeTestDefaultStakeWeight,
		},
		StakeOuts: stakes,
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, 3717*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())),
						1090,
						266,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, subnetAuth := txsCreationHelpers(defaultCtx)
	uTx := &txs.CreateChainTx{
		BaseTx:      baseTx,
		SubnetID:    ids.GenerateTestID(),
		ChainName:   "testingStuff",
		VMID:        ids.GenerateTestID(),
		FxIDs:       []ids.ID{ids.GenerateTestID()},
		GenesisData: []byte{0xff},
		SubnetAuth:  subnetAuth,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.CreateBlockchainTxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.CreateSubnetTx{
		BaseTx: baseTx,
		Owner: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.CreateSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, auth := txsCreationHelpers(defaultCtx)
	uTx := &txs.RemoveSubnetValidatorTx{
		BaseTx:     baseTx,
		NodeID:     ids.GenerateTestNodeID(),
		Subnet:     ids.GenerateTestID(),
		SubnetAuth: auth,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, auth := txsCreationHelpers(defaultCtx)
	uTx := &txs.TransformSubnetTx{
		BaseTx:                   baseTx,
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
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TransformSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.TransferSubnetOwnershipTx{
		BaseTx: baseTx,
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
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, stakes, _ := txsCreationHelpers(defaultCtx)
	sk, err := bls.NewSecretKey()
	r.NoError(err)
	uTx := &txs.AddPermissionlessValidatorTx{
		BaseTx:    baseTx,
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
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						266,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, stakes, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: baseTx,
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
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.AddSubnetDelegatorFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						266,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &baseTx
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						1090,
						172,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.ImportTx{
		BaseTx:      baseTx,
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
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
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
						uint64(len(stx.Bytes())),
						2180,
						262,
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
					FeeConfig:   feeTestsDefaultCfg,
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

	baseTx, outputs, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ids.GenerateTestID(),
		ExportedOutputs:  outputs,
	}
	stx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := &config.Config{
					FeeConfig:   feeTestsDefaultCfg,
					DurangoTime: time.Time{}, // durango already active
					EForkTime:   eForkTime,
				}

				return cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *FeeCalculator) {
				require.Equal(t, 3665*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						uint64(len(stx.Bytes())),
						1090,
						266,
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
					FeeConfig:   feeTestsDefaultCfg,
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

func txsCreationHelpers(defaultCtx *snow.Context) (
	baseTx txs.BaseTx,
	stakes []*avax.TransferableOutput,
	auth *secp256k1fx.Input,
) {
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
	stakes = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(time.Now().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: feeTestDefaultStakeWeight,
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
	baseTx = txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		},
	}

	return baseTx, stakes, auth
}
