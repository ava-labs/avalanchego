// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
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
	expectedError       error
	checksF             func(*testing.T, *Calculator)
}

func TestTxFees(t *testing.T) {
	r := require.New(t)

	tests := []feeTests{
		{
			description: "AddValidatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddSubnetValidatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddDelegatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: addDelegatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			description: "CreateChainTx pre ApricotPhase3",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				apricotPhase3Time := time.Now().Truncate(time.Second)
				chainTime := apricotPhase3Time.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.ApricotPhase3Time = apricotPhase3Time
				cfg.DurangoTime = mockable.MaxTime
				cfg.EUpgradeTime = mockable.MaxTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createChainTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.CreateAssetTxFee, fc.Fee)
			},
		},
		{
			description: "CreateChainTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createChainTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.CreateBlockchainTxFee, fc.Fee)
			},
		},
		{
			description: "CreateSubnetTx pre ApricotPhase3",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				apricotPhase3Time := time.Now().Truncate(time.Second)
				chainTime := apricotPhase3Time.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.ApricotPhase3Time = apricotPhase3Time
				cfg.DurangoTime = mockable.MaxTime
				cfg.EUpgradeTime = mockable.MaxTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.CreateAssetTxFee, fc.Fee)
			},
		},
		{
			description: "CreateSubnetTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: createSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.CreateSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "RemoveSubnetValidatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: removeSubnetValidatorTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TxFee, fc.Fee)
			},
		},
		{
			description: "TransformSubnetTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transformSubnetTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TransformSubnetTxFee, fc.Fee)
			},
		},
		{
			description: "TransferSubnetOwnershipTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: transferSubnetOwnershipTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TxFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessValidatorTx Primary Network pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessValidatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessValidatorTx Subnet pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(t, subnetID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessDelegatorTx Primary Network pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return addPermissionlessDelegatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			description: "AddPermissionlessDelegatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(t *testing.T) (txs.UnsignedTx, *txs.Tx) {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessDelegatorTx(t, subnetID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.AddSubnetDelegatorFee, fc.Fee)
			},
		},
		{
			description: "BaseTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: baseTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TxFee, fc.Fee)
			},
		},
		{
			description: "ImportTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: importTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TxFee, fc.Fee)
			},
		},
		{
			description: "ExportTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: exportTx,
			expectedError:       nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.config.TxFee, fc.Fee)
			},
		},
		{
			description: "RewardValidatorTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(_ *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				}, nil
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, uint64(0), fc.Fee)
			},
		},
		{
			description: "AdvanceTimeTx pre EUpgrade",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eUpgradeTime := time.Now().Truncate(time.Second)
				chainTime := eUpgradeTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.DurangoTime = durangoTime
				cfg.EUpgradeTime = eUpgradeTime

				return &cfg, chainTime
			},
			unsignedAndSignedTx: func(_ *testing.T) (txs.UnsignedTx, *txs.Tx) {
				return &txs.AdvanceTimeTx{
					Time: uint64(time.Now().Unix()),
				}, nil
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, uint64(0), fc.Fee)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			uTx, _ := tt.unsignedAndSignedTx(t)
			fc := NewStaticCalculator(cfg, chainTime)

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
