// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

func ExampleNewCalculator() {
	const (
		day             = 24 * time.Hour
		week            = 7 * day
		stakingDuration = 4 * week

		stakeAmount = 100_000 * units.Avax // 100k AVAX

		// The current supply can be fetched with the platform.getCurrentSupply API
		currentSupply = 447_903_489_576_595_361 * units.NanoAvax // ~448m AVAX
	)
	var (
		mainnetRewardConfig = Config{
			MaxConsumptionRate: .12 * PercentDenominator,
			MinConsumptionRate: .10 * PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}
		mainnetCalculator = NewCalculator(mainnetRewardConfig)
	)

	potentialReward := mainnetCalculator.Calculate(stakingDuration, stakeAmount, currentSupply)

	fmt.Printf("Staking %d nAVAX for %s with the current supply of %d nAVAX would have a potential reward of %d nAVAX",
		stakeAmount,
		stakingDuration,
		currentSupply,
		potentialReward,
	)
	// Output: Staking 100000000000000 nAVAX for 672h0m0s with the current supply of 447903489576595361 nAVAX would have a potential reward of 473168956104 nAVAX
}
