// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	//go:embed genesis_camino.json
	caminoGenesisConfigJSON []byte

	// CaminoParams are the params used for camino main network
	CaminoParams = Params{
		TxFeeConfig: TxFeeConfig{
			TxFee:                 units.MilliAvax,
			CreateAssetTxFee:      units.MilliAvax,
			CreateSubnetTxFee:     100 * units.MilliAvax,
			CreateBlockchainTxFee: 100 * units.MilliAvax,
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 100 * units.KiloAvax,
			MaxValidatorStake: 100 * units.KiloAvax,
			MinDelegatorStake: 0,
			MinDelegationFee:  0,
			MinStakeDuration:  182 * 24 * time.Hour,
			MaxStakeDuration:  5 * 365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: 0,
				MinConsumptionRate: 0,
				MintingPeriod:      5 * 365 * 24 * time.Hour,
				SupplyCap:          1000 * units.MegaAvax,
			},
			CaminoConfig: caminoconfig.Config{
				DACProposalBondAmount: 1 * units.KiloAvax,
			},
		},
	}
)
