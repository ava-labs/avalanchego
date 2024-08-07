// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

// PrivateKey-vmRQiZeXEXYMyJhEiqdC2z5JhuDbxL8ix9UVvjgMu2Er1NepE => X-kopernikus1g65uqn6t77p656w64023nh8nd9updzmxh8ttv3
// PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN => X-kopernikus18jma8ppw3nhx5r4ap8clazz0dps7rv5uuvjh68
// staking/local/staker1.key / crt => NodeID-AK7sPBsZM9rQwse23aLhEEBPHZD5gkLrL => PrivateKey-26ksbvjbz8jUTtzbCm3MYobKcDh22QPuPQX5dj2faQdR63TRdM
// staking/local/staker2.key / crt => NodeID-D1LbWvUf9iaeEyUbTYYtYq4b7GaYR5tnJ => PrivateKey-2ZW6HUePBW2dP7dBGa5stjXe1uvK9LwEgrjebDwXEyL5bDMWWS
// 56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027 => 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/caminoconfig"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
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
			CaminoConfig: caminoconfig.Config{
				DACProposalBondAmount: 100 * units.Avax,
				FeeDistribution:       [dac.FeeDistributionFractionsCount]uint64{30, 30, 40}, // 30% validators, 30% grant program, 40% burned
			},
		},
	}
)
