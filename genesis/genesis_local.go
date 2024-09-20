// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"

	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	validatorfee "github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

// PrivateKey-vmRQiZeXEXYMyJhEiqdC2z5JhuDbxL8ix9UVvjgMu2Er1NepE => P-local1g65uqn6t77p656w64023nh8nd9updzmxyymev2
// PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN => X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u
// 56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027 => 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC

const (
	VMRQKeyStr          = "vmRQiZeXEXYMyJhEiqdC2z5JhuDbxL8ix9UVvjgMu2Er1NepE"
	VMRQKeyFormattedStr = secp256k1.PrivateKeyPrefix + VMRQKeyStr

	EWOQKeyStr          = "ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	EWOQKeyFormattedStr = secp256k1.PrivateKeyPrefix + EWOQKeyStr
)

var (
	VMRQKey *secp256k1.PrivateKey
	EWOQKey *secp256k1.PrivateKey

	//go:embed genesis_local.json
	localGenesisConfigJSON []byte

	// LocalParams are the params used for local networks
	LocalParams = Params{
		TxFeeConfig: TxFeeConfig{
			CreateAssetTxFee: units.MilliAvax,
			StaticFeeConfig: txfee.StaticConfig{
				TxFee:                         units.MilliAvax,
				CreateSubnetTxFee:             100 * units.MilliAvax,
				TransformSubnetTxFee:          100 * units.MilliAvax,
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
			ValidatorFeeCapacity: 20_000,
			ValidatorFeeConfig: validatorfee.Config{
				Target:   10_000,
				MinPrice: gas.Price(1 * units.NanoAvax),
				// ExcessConversionConstant = (Capacity - Target) * NumberOfSecondsPerDoubling / ln(2)
				//
				// ln(2) is a float and the result is consensus critical, so we
				// hardcode the result.
				ExcessConversionConstant: 865_617, // Double every minute
			},
		},
		StakingConfig: StakingConfig{
			UptimeRequirement: .8, // 80%
			MinValidatorStake: 2 * units.KiloAvax,
			MaxValidatorStake: 3 * units.MegaAvax,
			MinDelegatorStake: 25 * units.Avax,
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

func init() {
	errs := wrappers.Errs{}
	vmrqBytes, err := cb58.Decode(VMRQKeyStr)
	errs.Add(err)
	ewoqBytes, err := cb58.Decode(EWOQKeyStr)
	errs.Add(err)

	VMRQKey, err = secp256k1.ToPrivateKey(vmrqBytes)
	errs.Add(err)
	EWOQKey, err = secp256k1.ToPrivateKey(ewoqBytes)
	errs.Add(err)

	if errs.Err != nil {
		panic(errs.Err)
	}
}
