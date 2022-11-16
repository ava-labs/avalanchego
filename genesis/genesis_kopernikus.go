// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	//go:embed genesis_kopernikus.json
	kopernikusGenesisConfigJSON []byte

	// KoperikusParams are the params used for kopernikus dev network
	KopernikusParams = Params{
		TxFeeConfig: TxFeeConfig{
			TxFee:                 units.MilliAvax,
			CreateAssetTxFee:      units.MilliAvax,
			CreateSubnetTxFee:     100 * units.MilliAvax,
			CreateBlockchainTxFee: 100 * units.MilliAvax,
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 2 * units.KiloAvax,
			MaxValidatorStake: 2 * units.KiloAvax,
			MinDelegatorStake: 0 * units.Avax,
			MinDelegationFee:  0, // 0%
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: 0,
				MinConsumptionRate: 0,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          1000 * units.MegaAvax,
			},
			CaminoConfig: config.CaminoConfig{
				DaoProposalBondAmount: 100 * units.Avax,
			},
		},
	}
)
