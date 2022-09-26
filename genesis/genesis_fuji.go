// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"

	xchainconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	pchainconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var (
	//go:embed genesis_fuji.json
	fujiGenesisConfigJSON []byte

	// FujiParams are the params used for the fuji testnet
	FujiParams = Params{
		PChainTxFees: pchainconfig.TxFeeUpgrades{
			InitialFees: pchainconfig.TxFees{
				AddPrimaryNetworkValidator: 0,
				AddPrimaryNetworkDelegator: 0,
				AddPOASubnetValidator:      units.MilliAvax,
				AddPOSSubnetValidator:      units.MilliAvax, // didn't exist
				AddPOSSubnetDelegator:      units.MilliAvax, // didn't exist
				RemovePOASubnetValidator:   units.MilliAvax, // didn't exist
				CreateSubnet:               10 * units.MilliAvax,
				CreateChain:                10 * units.MilliAvax,
				TransformSubnet:            10 * units.MilliAvax, // didn't exist
				Import:                     units.MilliAvax,
				Export:                     units.MilliAvax,
			},
			ApricotPhase3Fees: pchainconfig.TxFees{
				AddPrimaryNetworkValidator: 0,
				AddPrimaryNetworkDelegator: 0,
				AddPOASubnetValidator:      units.MilliAvax,
				AddPOSSubnetValidator:      units.MilliAvax, // didn't exist
				AddPOSSubnetDelegator:      units.MilliAvax, // didn't exist
				RemovePOASubnetValidator:   units.MilliAvax, // didn't exist
				CreateSubnet:               100 * units.MilliAvax,
				CreateChain:                100 * units.MilliAvax,
				TransformSubnet:            100 * units.MilliAvax, // didn't exist
				Import:                     units.MilliAvax,
				Export:                     units.MilliAvax,
			},
			BlueberryFees: pchainconfig.TxFees{
				AddPrimaryNetworkValidator: 0,
				AddPrimaryNetworkDelegator: 0,
				AddPOASubnetValidator:      units.MilliAvax,
				AddPOSSubnetValidator:      units.MilliAvax,
				AddPOSSubnetDelegator:      units.MilliAvax,
				RemovePOASubnetValidator:   units.MilliAvax,
				CreateSubnet:               100 * units.MilliAvax,
				CreateChain:                100 * units.MilliAvax,
				TransformSubnet:            100 * units.MilliAvax,
				Import:                     units.MilliAvax,
				Export:                     units.MilliAvax,
			},
		},
		XChainTxFees: xchainconfig.TxFees{
			Base:        units.MilliAvax,
			CreateAsset: 10 * units.MilliAvax,
			Operation:   units.MilliAvax,
			Import:      units.MilliAvax,
			Export:      units.MilliAvax,
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
