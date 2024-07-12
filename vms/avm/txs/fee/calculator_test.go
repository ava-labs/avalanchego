// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

func TestTxFees(t *testing.T) {
	feeTestsDefaultCfg := StaticConfig{
		TxFee:            1 * units.Avax,
		CreateAssetTxFee: 2 * units.Avax,
	}

	// chain times needed to have specific upgrades active
	eUpgradeTime := time.Unix(1713945427, 0)
	preEUpgradeTime := eUpgradeTime.Add(-1 * time.Second)

	tests := []struct {
		name       string
		chainTime  time.Time
		unsignedTx func() txs.UnsignedTx
		expected   uint64
	}{
		{
			name:       "BaseTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: baseTx,
			expected:   feeTestsDefaultCfg.TxFee,
		},
		{
			name:       "CreateAssetTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: createAssetTx,
			expected:   feeTestsDefaultCfg.CreateAssetTxFee,
		},
		{
			name:       "OperationTx pre EUpgrade",
			chainTime:  preEUpgradeTime,
			unsignedTx: operationTx,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uTx := tt.unsignedTx()
			fc := NewStaticCalculator(feeTestsDefaultCfg)
			haveFee, err := fc.CalculateFee(&txs.Tx{Unsigned: uTx})
			require.NoError(t, err)
			require.Equal(t, tt.expected, haveFee)
		})
	}
}

func baseTx() txs.UnsignedTx {
	return &txs.BaseTx{}
}

func createAssetTx() txs.UnsignedTx {
	return &txs.CreateAssetTx{}
}

func operationTx() txs.UnsignedTx {
	return &txs.OperationTx{}
}

func importTx() txs.UnsignedTx {
	return &txs.ImportTx{}
}

func exportTx() txs.UnsignedTx {
	return &txs.ExportTx{}
}
