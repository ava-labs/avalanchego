// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

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
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	testFeeRates = fees.Dimensions{
		1 * units.MicroAvax,
		2 * units.MicroAvax,
		3 * units.MicroAvax,
		4 * units.MicroAvax,
	}
	testBlockMaxConsumedUnits = fees.Dimensions{
		3000,
		3500,
		1000,
		2000,
	}

	feeTestsDefaultCfg = config.Config{
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

	preFundedKeys             = secp256k1.TestKeys()
	feeTestSigners            = [][]*secp256k1.PrivateKey{preFundedKeys}
	feeTestDefaultStakeWeight = uint64(2024)
	durangoTime               = time.Time{} // assume durango is active in these tests
)

type feeTests struct {
	description         string
	cfgAndChainTimeF    func() (*config.Config, time.Time)
	unsignedAndSignedTx func(t *testing.T) (txs.UnsignedTx, *txs.Tx)
	consumedUnitCapsF   func() fees.Dimensions
	expectedError       error
	checksF             func(*testing.T, *Calculator)
}

func TestAddAndRemoveFees(t *testing.T) {
	r := require.New(t)

	fc := &Calculator{
		IsEUpgradeActive: true,
		FeeManager:       fees.NewManager(testFeeRates),
		ConsumedUnitsCap: testBlockMaxConsumedUnits,
	}

	var (
		units       = fees.Dimensions{1, 2, 3, 4}
		doubleUnits = fees.Dimensions{2, 4, 6, 8}
	)

	feeDelta, err := fc.AddFeesFor(units)
	r.NoError(err)
	r.Equal(units, fc.FeeManager.GetCumulatedComplexity())
	r.NotZero(feeDelta)
	r.Equal(feeDelta, fc.Fee)

	feeDelta2, err := fc.AddFeesFor(units)
	r.NoError(err)
	r.Equal(doubleUnits, fc.FeeManager.GetCumulatedComplexity())
	r.Equal(feeDelta, feeDelta2)
	r.Equal(feeDelta+feeDelta2, fc.Fee)

	feeDelta3, err := fc.RemoveFeesFor(units)
	r.NoError(err)
	r.Equal(units, fc.FeeManager.GetCumulatedComplexity())
	r.Equal(feeDelta, feeDelta3)
	r.Equal(feeDelta, fc.Fee)

	feeDelta4, err := fc.RemoveFeesFor(units)
	r.NoError(err)
	r.Zero(fc.FeeManager.GetCumulatedComplexity())
	r.Equal(feeDelta, feeDelta4)
	r.Zero(fc.Fee)
}

func TestTxFees(t *testing.T) {
	r := require.New(t)

	tests := []feeTests{
		{
			description: "AddValidatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddValidatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError:       nil,
			unsignedAndSignedTx: addValidatorTx,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddSubnetValidatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddSubnetValidatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError:       nil,
			unsignedAndSignedTx: addSubnetValidatorTx,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5345*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						649,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "AddSubnetValidatorTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: addSubnetValidatorTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "AddDelegatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addDelegatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			description: "AddDelegatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addDelegatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			description: "CreateChainTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createChainTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.CreateBlockchainTxFee, fc.Fee)
			},
		},
		{
			description: "CreateChainTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createChainTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5388*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						692,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "CreateChainTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createChainTx,
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
		{
			description: "CreateSubnetTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.CreateSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "CreateSubnetTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5293*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						597,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "CreateSubnetTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "RemoveSubnetValidatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "RemoveSubnetValidatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5321*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						625,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "RemoveSubnetValidatorTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "TransformSubnetTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TransformSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "TransformSubnetTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5406*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						710,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "TransformSubnetTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "TransferSubnetOwnershipTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "TransferSubnetOwnershipTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5337*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						641,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "TransferSubnetOwnershipTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "AddPermissionlessValidatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addPermissionlessValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessValidatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addPermissionlessValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5939*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						961,
						90,
						266,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "AddPermissionlessValidatorTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: addPermissionlessValidatorTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "AddPermissionlessDelegatorTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addPermissionlessDelegatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.AddSubnetDelegatorFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessDelegatorTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addPermissionlessDelegatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5747*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						769,
						90,
						266,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "AddPermissionlessDelegatorTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: addPermissionlessDelegatorTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "BaseTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: baseTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "BaseTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: baseTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5253*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						557,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "BaseTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: baseTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "ImportTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: importTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "ImportTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: importTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 9827*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						681,
						180,
						262,
						2000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "ImportTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 180 - 1
				return caps
			},
			unsignedAndSignedTx: importTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
		{
			description: "ExportTx pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: exportTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "ExportTx post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: exportTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5663*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						685,
						90,
						266,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "ExportTx post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			unsignedAndSignedTx: exportTx,
			expectedError:       errFailedConsumedUnitsCumulation,
			checksF:             func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			uTx, sTx := tt.unsignedAndSignedTx(t)

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				ChainTime:        chainTime,
				FeeManager:       fees.NewManager(testFeeRates),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func addValidatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)

	return uTx, sTx
}

func addSubnetValidatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func addDelegatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func createChainTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func createSubnetTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func removeSubnetValidatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, _, auth := txsCreationHelpers(defaultCtx)
	uTx := &txs.RemoveSubnetValidatorTx{
		BaseTx:     baseTx,
		NodeID:     ids.GenerateTestNodeID(),
		Subnet:     ids.GenerateTestID(),
		SubnetAuth: auth,
	}
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func transformSubnetTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func transferSubnetOwnershipTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func addPermissionlessValidatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func addPermissionlessDelegatorTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func baseTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &baseTx
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func importTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
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
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
}

func exportTx(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, outputs, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ids.GenerateTestID(),
		ExportedOutputs:  outputs,
	}
	sTx, err := txs.NewSigned(uTx, txs.Codec, feeTestSigners)
	r.NoError(err)
	return uTx, sTx
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
