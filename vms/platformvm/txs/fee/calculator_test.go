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
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	preFundedKeys             = secp256k1.TestKeys()
	feeTestDefaultStakeWeight = uint64(2024)
)

func TestTxFees(t *testing.T) {
	r := require.New(t)

	type feeTests struct {
		name          string
		chainTime     time.Time
		unsignedTx    func(t *testing.T) txs.UnsignedTx
		expectedError error
		checksF       func(*testing.T, *Calculator)
	}

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

	tests := []feeTests{
		{
			name:          "AddValidatorTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    addValidatorTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			name:          "AddSubnetValidatorTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    addSubnetValidatorTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			name:          "AddDelegatorTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    addDelegatorTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			name:          "CreateChainTx pre ApricotPhase3",
			chainTime:     upgrades.ApricotPhase3Time.Add(-1 * time.Second),
			unsignedTx:    createChainTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.CreateAssetTxFee, fc.Fee)
			},
		},
		{
			name:          "CreateChainTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    createChainTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.CreateBlockchainTxFee, fc.Fee)
			},
		},
		{
			name:          "CreateSubnetTx pre ApricotPhase3",
			chainTime:     upgrades.ApricotPhase3Time.Add(-1 * time.Second),
			unsignedTx:    createSubnetTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.CreateAssetTxFee, fc.Fee)
			},
		},
		{
			name:          "CreateSubnetTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    createSubnetTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.CreateSubnetTxFee, fc.Fee)
			},
		},
		{
			name:          "RemoveSubnetValidatorTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    removeSubnetValidatorTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.Fee)
			},
		},
		{
			name:          "TransformSubnetTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    transformSubnetTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TransformSubnetTxFee, fc.Fee)
			},
		},
		{
			name:          "TransferSubnetOwnershipTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    transferSubnetOwnershipTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.Fee)
			},
		},
		{
			name:      "AddPermissionlessValidatorTx Primary Network pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(t *testing.T) txs.UnsignedTx {
				return addPermissionlessValidatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkValidatorFee, fc.Fee)
			},
		},
		{
			name:      "AddPermissionlessValidatorTx Subnet pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(t *testing.T) txs.UnsignedTx {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(t, subnetID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetValidatorFee, fc.Fee)
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx Primary Network pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(t *testing.T) txs.UnsignedTx {
				return addPermissionlessDelegatorTx(t, constants.PrimaryNetworkID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddPrimaryNetworkDelegatorFee, fc.Fee)
			},
		},
		{
			name:      "AddPermissionlessDelegatorTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(t *testing.T) txs.UnsignedTx {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessDelegatorTx(t, subnetID)
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.AddSubnetDelegatorFee, fc.Fee)
			},
		},
		{
			name:          "BaseTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    baseTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.Fee)
			},
		},
		{
			name:          "ImportTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    importTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.Fee)
			},
		},
		{
			name:          "ExportTx pre EUpgrade",
			chainTime:     upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx:    exportTx,
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.staticCfg.TxFee, fc.Fee)
			},
		},
		{
			name:      "RewardValidatorTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(_ *testing.T) txs.UnsignedTx {
				return &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				}
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, uint64(0), fc.Fee)
			},
		},
		{
			name:      "AdvanceTimeTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func(_ *testing.T) txs.UnsignedTx {
				return &txs.AdvanceTimeTx{
					Time: uint64(time.Now().Unix()),
				}
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, uint64(0), fc.Fee)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uTx := tt.unsignedTx(t)
			fc := NewStaticCalculator(feeTestsDefaultCfg, upgrades, tt.chainTime)

			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func addValidatorTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func addSubnetValidatorTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func addDelegatorTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func createChainTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func createSubnetTx(t *testing.T) txs.UnsignedTx {
	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.CreateSubnetTx{
		BaseTx: baseTx,
		Owner: &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		},
	}
	return uTx
}

func removeSubnetValidatorTx(t *testing.T) txs.UnsignedTx {
	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, _, auth := txsCreationHelpers(defaultCtx)
	uTx := &txs.RemoveSubnetValidatorTx{
		BaseTx:     baseTx,
		NodeID:     ids.GenerateTestNodeID(),
		Subnet:     ids.GenerateTestID(),
		SubnetAuth: auth,
	}
	return uTx
}

func transformSubnetTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func transferSubnetOwnershipTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func addPermissionlessValidatorTx(t *testing.T, subnetID ids.ID) txs.UnsignedTx {
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
	return uTx
}

func addPermissionlessDelegatorTx(t *testing.T, subnetID ids.ID) txs.UnsignedTx {
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
	return uTx
}

func baseTx(t *testing.T) txs.UnsignedTx {
	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, _, _ := txsCreationHelpers(defaultCtx)
	uTx := &baseTx
	return uTx
}

func importTx(t *testing.T) txs.UnsignedTx {
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
	return uTx
}

func exportTx(t *testing.T) txs.UnsignedTx {
	defaultCtx := snowtest.Context(t, snowtest.PChainID)

	baseTx, outputs, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ids.GenerateTestID(),
		ExportedOutputs:  outputs,
	}
	return uTx
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
