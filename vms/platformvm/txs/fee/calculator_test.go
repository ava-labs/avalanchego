// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	testGasPrice = fees.GasPrice(10 * units.NanoAvax)

	testBlockMaxGas = fees.Gas(100_000)

	preFundedKeys             = secp256k1.TestKeys()
	feeTestSigners            = [][]*secp256k1.PrivateKey{preFundedKeys}
	feeTestDefaultStakeWeight = uint64(2024)

	errFailedComplexityCumulation = errors.New("failed cumulating complexity")
)

func TestAddAndRemoveFees(t *testing.T) {
	r := require.New(t)

	fc := &Calculator{
		c: &calculator{
			isEActive:  true,
			feeManager: fees.NewManager(testGasPrice),
			maxGas:     testBlockMaxGas,
		},
	}

	var (
		units     = fees.Dimensions{1, 2, 3, 4}
		gas       = fees.Gas(10)
		doubleGas = fees.Gas(20)
	)

	feeDelta, err := fc.AddFeesFor(units)
	r.NoError(err)
	r.Equal(gas, fc.c.feeManager.GetGas())
	r.NotZero(feeDelta)
	r.Equal(feeDelta, fc.c.fee)

	feeDelta2, err := fc.AddFeesFor(units)
	r.NoError(err)
	r.Equal(doubleGas, fc.c.feeManager.GetGas())
	r.Equal(feeDelta, feeDelta2)
	r.Equal(feeDelta+feeDelta2, fc.c.fee)

	feeDelta3, err := fc.RemoveFeesFor(units)
	r.NoError(err)
	r.Equal(gas, fc.c.feeManager.GetGas())
	r.Equal(feeDelta, feeDelta3)
	r.Equal(feeDelta, fc.c.fee)

	feeDelta4, err := fc.RemoveFeesFor(units)
	r.NoError(err)
	r.Zero(fc.c.feeManager.GetGas())
	r.Equal(feeDelta, feeDelta4)
	r.Zero(fc.c.fee)
}

func TestTxFees(t *testing.T) {
	feeTestsDefaultCfg := StaticConfig{
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

	latestForkTime := time.Unix(1713945427, 0)
	upgrades := upgrade.Config{
		EUpgradeTime:      latestForkTime,
		DurangoTime:       latestForkTime.Add(-1 * time.Hour),
		CortinaTime:       latestForkTime.Add(-2 * time.Hour),
		BanffTime:         latestForkTime.Add(-3 * time.Hour),
		ApricotPhase5Time: latestForkTime.Add(-4 * time.Hour),
		ApricotPhase3Time: latestForkTime.Add(-5 * time.Hour),
	}

	// chain times needed to have specific upgrades active
	postEUpgradeTime := upgrades.EUpgradeTime.Add(time.Second)
	preEUpgradeTime := upgrades.EUpgradeTime.Add(-1 * time.Second)
	preApricotPhase3Time := upgrades.ApricotPhase3Time.Add(-1 * time.Second)

	tests := []struct {
		name                string
		chainTime           time.Time
		unsignedAndSignedTx func(t *testing.T) (txs.UnsignedTx, *txs.Tx)
		maxGasF             func() fees.Gas
		expectedError       error
		checksF             func(*testing.T, *calculator)
	}{
		{
			name:                "AddValidatorTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: addValidatorTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkValidatorFee, fc.fee)
			},
		},
		{
			name:                "AddSubnetValidatorTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: addSubnetValidatorTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetValidatorFee, fc.fee)
			},
		},
		{
			name:                "AddSubnetValidatorTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			expectedError:       nil,
			unsignedAndSignedTx: addSubnetValidatorTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 19_110*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1911), fc.feeManager.GetGas())
			},
		},
		{
			name:      "AddSubnetValidatorTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: addSubnetValidatorTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:                "AddDelegatorTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: addDelegatorTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkDelegatorFee, fc.fee)
			},
		},
		{
			name:                "CreateChainTx pre ApricotPhase3",
			chainTime:           preApricotPhase3Time,
			unsignedAndSignedTx: createChainTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.CreateAssetTxFee, fc.fee)
			},
		},
		{
			name:                "CreateChainTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: createChainTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.CreateBlockchainTxFee, fc.fee)
			},
		},
		{
			name:                "CreateChainTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: createChainTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 19_540*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_954), fc.feeManager.GetGas())
			},
		},
		{
			name:                "CreateChainTx post EUpgrade, utxos read cap breached",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: createChainTx,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			expectedError: errFailedComplexityCumulation,
			checksF:       func(*testing.T, *calculator) {},
		},
		{
			name:                "CreateSubnetTx pre ApricotPhase3",
			chainTime:           preApricotPhase3Time,
			unsignedAndSignedTx: createSubnetTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.CreateAssetTxFee, fc.fee)
			},
		},
		{
			name:                "CreateSubnetTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: createSubnetTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.CreateSubnetTxFee, fc.fee)
			},
		},
		{
			name:                "CreateSubnetTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 18_590*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_859), fc.feeManager.GetGas())
			},
		},
		{
			name:                "CreateSubnetTx post EUpgrade, utxos read cap breached",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: createSubnetTx,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			expectedError: errFailedComplexityCumulation,
			checksF:       func(*testing.T, *calculator) {},
		},
		{
			name:                "RemoveSubnetValidatorTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: removeSubnetValidatorTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.fee)
			},
		},
		{
			name:                "RemoveSubnetValidatorTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 18_870*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_887), fc.feeManager.GetGas())
			},
		},
		{
			name:      "RemoveSubnetValidatorTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:                "TransformSubnetTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: transformSubnetTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TransformSubnetTxFee, fc.fee)
			},
		},
		{
			name:                "TransformSubnetTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 19_720*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_972), fc.feeManager.GetGas())
			},
		},
		{
			name:      "TransformSubnetTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:                "TransferSubnetOwnershipTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.fee)
			},
		},
		{
			name:                "TransferSubnetOwnershipTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 19_030*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_903), fc.feeManager.GetGas())
			},
		},
		{
			name:      "TransferSubnetOwnershipTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:      "AddPermissionlessValidatorTx Primary Network pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessValidatorTx(t, constants.PrimaryNetworkID)
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkValidatorFee, fc.fee)
			},
		},
		{
			name:      "AddPermissionlessValidatorTx Subnet pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(t, subnetID)
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetValidatorFee, fc.fee)
			},
		},
		{
			name:      "AddPermissionlessValidatorTx Primary Network post EUpgrade, success",
			chainTime: postEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessValidatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 23_170*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(2_317), fc.feeManager.GetGas())
			},
		},
		{
			name:      "AddPermissionlessValidatorTx Subnet post EUpgrade, success",
			chainTime: postEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(t, subnetID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 23_170*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(2_317), fc.feeManager.GetGas())
			},
		},
		{
			name:      "AddPermissionlessValidatorTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				return addPermissionlessValidatorTx(t, subnetID)
			},
			expectedError: errFailedComplexityCumulation,
			checksF:       func(*testing.T, *calculator) {},
		},
		{
			name:      "AddPermissionlessDelegatorTx Primary Network pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessDelegatorTx(t, constants.PrimaryNetworkID)
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkDelegatorFee, fc.fee)
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessDelegatorTx(t, subnetID)
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetDelegatorFee, fc.fee)
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx Primary Network post EUpgrade, success",
			chainTime: postEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessDelegatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 21_250*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(2_125), fc.feeManager.GetGas())
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx Subnet post EUpgrade, success",
			chainTime: postEUpgradeTime,
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessDelegatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 21_250*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(2_125), fc.feeManager.GetGas())
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx Subnet post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(t, subnetID)
			},
			expectedError: errFailedComplexityCumulation,
			checksF:       func(*testing.T, *calculator) {},
		},
		{
			name:                "BaseTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: baseTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.fee)
			},
		},
		{
			name:                "BaseTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: baseTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 18_190*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(1_819), fc.feeManager.GetGas())
			},
		},
		{
			name:      "BaseTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: baseTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:                "ImportTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: importTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.fee)
			},
		},
		{
			name:                "ImportTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: importTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 31_230*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(3_123), fc.feeManager.GetGas())
			},
		},
		{
			name:      "ImportTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: importTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:                "ExportTx pre EUpgrade",
			chainTime:           preEUpgradeTime,
			unsignedAndSignedTx: exportTx,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.fee)
			},
		},
		{
			name:                "ExportTx post EUpgrade, success",
			chainTime:           postEUpgradeTime,
			unsignedAndSignedTx: exportTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, 20_410*units.NanoAvax, fc.fee)
				require.Equal(t, fees.Gas(2_041), fc.feeManager.GetGas())
			},
		},
		{
			name:      "ExportTx post EUpgrade, utxos read cap breached",
			chainTime: postEUpgradeTime,
			maxGasF: func() fees.Gas {
				return testBlockMaxGas - 1
			},
			unsignedAndSignedTx: exportTx,
			expectedError:       errFailedComplexityCumulation,
			checksF:             func(*testing.T, *calculator) {},
		},
		{
			name:      "RewardValidatorTx pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(_ *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				}, nil
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, uint64(0), fc.fee)
			},
		},
		{
			name:      "AdvanceTimeTx pre EUpgrade",
			chainTime: preEUpgradeTime,
			unsignedAndSignedTx: func(_ *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return &txs.AdvanceTimeTx{
					Time: uint64(time.Now().Unix()),
				}, nil
			},
			checksF: func(t *testing.T, fc *calculator) {
				require.Equal(t, uint64(0), fc.fee)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxComplexity := testBlockMaxGas
			if tt.maxGasF != nil {
				maxComplexity = tt.maxGasF()
			}

			uTx, sTx := tt.unsignedAndSignedTx(t)

			var fc *Calculator
			if !upgrades.IsEActivated(tt.chainTime) {
				fc = NewStaticCalculator(feeTestsDefaultCfg, upgrades, tt.chainTime)
			} else {
				fc = NewDynamicCalculator(fees.NewManager(testGasPrice), maxComplexity)
			}

			var creds []verify.Verifiable
			if sTx != nil {
				// txs like RewardValidatorTx are not signed
				creds = sTx.Creds
			}
			_, _ = fc.ComputeFee(uTx, creds)
			tt.checksF(t, fc.c)
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

func addPermissionlessValidatorTx(t *testing.T, subnetID ids.ID) (txs.UnsignedTx, *txs.Tx) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, stakes, _ := txsCreationHelpers(defaultCtx)
	sk, err := bls.NewSecretKey()
	r.NoError(err)
	uTx := &txs.AddPermissionlessValidatorTx{
		BaseTx:    baseTx,
		Subnet:    subnetID,
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

func addPermissionlessDelegatorTx(t *testing.T, subnetID ids.ID) (txs.UnsignedTx, *txs.Tx) {
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
		Subnet:    subnetID,
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
