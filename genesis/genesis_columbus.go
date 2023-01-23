// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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
	//go:embed genesis_columbus.json
	columbusGenesisConfigJSON []byte

	// ColumbusParams are the params used for columbus network
	ColumbusParams = Params{
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
			MinDelegatorStake: 25 * units.Avax,
			MinDelegationFee:  20000, // 2%
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .12 * reward.PercentDenominator,
				MinConsumptionRate: .10 * reward.PercentDenominator,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          1000 * units.MegaAvax,
			},
			CaminoConfig: config.CaminoConfig{
				DaoProposalBondAmount:  100 * units.Avax,
				ValidatorsRewardPeriod: 60 * 60, // seconds
			},
		},
	}
)
