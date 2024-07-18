// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/upgrade"
)

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
	preEUpgradeTime := upgrades.EUpgradeTime.Add(-1 * time.Second)
	preApricotPhase3Time := upgrades.ApricotPhase3Time.Add(-1 * time.Second)

	tests := []struct {
		name       string
		chainTime  time.Time
		unsignedTx func() txs.UnsignedTx
		expected   uint64
	}{
		{
			name:       "AddValidatorTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: addValidatorTx,
			expected:   feeTestsDefaultCfg.AddPrimaryNetworkValidatorFee,
		},
		{
			name:       "AddSubnetValidatorTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: addSubnetValidatorTx,
			expected:   feeTestsDefaultCfg.AddSubnetValidatorFee,
		},
		{
			name:       "AddDelegatorTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: addDelegatorTx,
			expected:   feeTestsDefaultCfg.AddPrimaryNetworkDelegatorFee,
		},
		{
			name:       "CreateChainTx pre ApricotPhase3",
			chainTime:  preApricotPhase3Time,
			unsignedTx: createChainTx,
			expected:   feeTestsDefaultCfg.CreateAssetTxFee,
		},
		{
			name:       "CreateChainTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: createChainTx,
			expected:   feeTestsDefaultCfg.CreateBlockchainTxFee,
		},
		{
			name:       "CreateSubnetTx pre ApricotPhase3",
			chainTime:  preApricotPhase3Time,
			unsignedTx: createSubnetTx,
			expected:   feeTestsDefaultCfg.CreateAssetTxFee,
		},
		{
			name:       "CreateSubnetTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: createSubnetTx,
			expected:   feeTestsDefaultCfg.CreateSubnetTxFee,
		},
		{
			name:       "RemoveSubnetValidatorTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: removeSubnetValidatorTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:       "TransformSubnetTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: transformSubnetTx,
			expected:   feeTestsDefaultCfg.TransformSubnetTxFee,
		},
		{
			name:       "TransferSubnetOwnershipTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: transferSubnetOwnershipTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:      "AddPermissionlessValidatorTx Primary Network pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				return addPermissionlessValidatorTx(constants.PrimaryNetworkID)
			},
			expected: feeTestsDefaultCfg.AddPrimaryNetworkValidatorFee,
		},
		{
			name:      "AddPermissionlessValidatorTx Subnet pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessValidatorTx(subnetID)
			},
			expected: feeTestsDefaultCfg.AddSubnetValidatorFee,
		},
		{
			name:      "AddPermissionlessDelegatorTx Primary Network pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				return addPermissionlessDelegatorTx(constants.PrimaryNetworkID)
			},
			expected: feeTestsDefaultCfg.AddPrimaryNetworkDelegatorFee,
		},
		{
			name:      "AddPermissionlessDelegatorTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				subnetID := ids.GenerateTestID()
				require.NotEqual(t, constants.PrimaryNetworkID, subnetID)
				return addPermissionlessDelegatorTx(subnetID)
			},
			expected: feeTestsDefaultCfg.AddSubnetDelegatorFee,
		},
		{
			name:       "BaseTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: baseTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:       "ImportTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: importTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:       "ExportTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: exportTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:      "RewardValidatorTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				return &txs.RewardValidatorTx{
					TxID: ids.GenerateTestID(),
				}
			},
			expected: 0,
		},
		{
			name:      "AdvanceTimeTx pre EUpgrade",
			chainTime: upgrades.EUpgradeTime.Add(-1 * time.Second),
			unsignedTx: func() txs.UnsignedTx {
				return &txs.AdvanceTimeTx{
					Time: uint64(time.Now().Unix()),
				}
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uTx := tt.unsignedTx()
			fc := NewStaticCalculator(feeTestsDefaultCfg, upgrades, tt.chainTime)
			fee, err := fc.CalculateFee(&txs.Tx{Unsigned: uTx})
			require.NoError(t, err)
			require.Equal(t, tt.expected, fee)
		})
	}
}

func addValidatorTx() txs.UnsignedTx {
	return &txs.AddValidatorTx{}
}

func addSubnetValidatorTx() txs.UnsignedTx {
	return &txs.AddSubnetValidatorTx{}
}

func addDelegatorTx() txs.UnsignedTx {
	return &txs.AddDelegatorTx{}
}

func createChainTx() txs.UnsignedTx {
	return &txs.CreateChainTx{}
}

func createSubnetTx() txs.UnsignedTx {
	return &txs.CreateSubnetTx{}
}

func removeSubnetValidatorTx() txs.UnsignedTx {
	return &txs.RemoveSubnetValidatorTx{}
}

func transformSubnetTx() txs.UnsignedTx {
	return &txs.TransformSubnetTx{}
}

func transferSubnetOwnershipTx() txs.UnsignedTx {
	return &txs.TransferSubnetOwnershipTx{}
}

func addPermissionlessValidatorTx(subnetID ids.ID) txs.UnsignedTx {
	return &txs.AddPermissionlessValidatorTx{
		Subnet: subnetID,
	}
}

func addPermissionlessDelegatorTx(subnetID ids.ID) txs.UnsignedTx {
	return &txs.AddPermissionlessDelegatorTx{
		Subnet: subnetID,
	}
}

func baseTx() txs.UnsignedTx {
	return &txs.BaseTx{}
}

func importTx() txs.UnsignedTx {
	return &txs.ImportTx{}
}

func exportTx() txs.UnsignedTx {
	return &txs.ExportTx{}
}
