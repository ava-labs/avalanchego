// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var (
	//go:embed genesis_fuji.json
	fujiGenesisConfigJSON []byte

	// FujiParams are the params used for the fuji testnet
	FujiParams = Params{
		TxFeeConfig: TxFeeConfig{
			CreateAssetTxFee: 10 * units.MilliAvax,
			StaticFeeConfig: fee.StaticConfig{
				TxFee:                         units.MilliAvax,
				CreateSubnetTxFee:             100 * units.MilliAvax,
				TransformSubnetTxFee:          1 * units.Avax,
				CreateBlockchainTxFee:         100 * units.MilliAvax,
				AddPrimaryNetworkValidatorFee: 0,
				AddPrimaryNetworkDelegatorFee: 0,
				AddSubnetValidatorFee:         units.MilliAvax,
				AddSubnetDelegatorFee:         units.MilliAvax,
			},
			// TODO: Set these values to something more reasonable
			DynamicFeeConfig: gas.Config{
				Weights: gas.Dimensions{
					gas.Bandwidth: 1,
					gas.DBRead:    1,
					gas.DBWrite:   1,
					gas.Compute:   1,
				},
				MaxCapacity:              1_000_000,
				MaxPerSecond:             1_000,
				TargetPerSecond:          500,
				MinPrice:                 1,
				ExcessConversionConstant: 5_000,
			},
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 1 * units.Avax,
			MaxValidatorStake: 3 * units.MegaAvax,
			MinDelegatorStake: 1 * units.Avax,
			MinDelegationFee:  20000, // 2%
			MinStakeDuration:  24 * time.Hour,
			MaxStakeDuration:  365 * 24 * time.Hour,
			RewardConfig: reward.Config{
				MaxConsumptionRate: .12 * reward.PercentDenominator,
				MinConsumptionRate: .10 * reward.PercentDenominator,
				MintingPeriod:      365 * 24 * time.Hour,
				SupplyCap:          720 * units.MegaAvax,
			},
		},
	}
)
