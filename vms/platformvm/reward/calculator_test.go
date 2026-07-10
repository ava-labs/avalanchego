// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultMinValidatorStake = 5 * units.MilliAvax
)

var defaultConfig = Config{
	MaxConsumptionRate: .12 * PercentDenominator,
	MinConsumptionRate: .10 * PercentDenominator,
	MintingPeriod:      365 * 24 * time.Hour,
	SupplyCap:          720 * units.MegaAvax,
}

type calculatorImplementation struct {
	name       string
	calculator Calculator
}

func newCalculatorsBeforeHelicon(config Config) []calculatorImplementation {
	heliconTime := time.Time{}.Add(time.Second)
	return []calculatorImplementation{
		{
			name:       "generic",
			calculator: NewCalculator(config),
		},
		{
			name: "primary_network_before_helicon",
			calculator: NewPrimaryNetworkCalculator(
				config,
				upgradetest.GetConfigWithUpgradeTime(upgradetest.Helicon, heliconTime),
			),
		},
	}
}

func TestLongerDurationBonus(t *testing.T) {
	for _, impl := range newCalculatorsBeforeHelicon(defaultConfig) {
		t.Run(impl.name, func(t *testing.T) {
			shortDuration := 24 * time.Hour
			totalDuration := 365 * 24 * time.Hour
			shortBalance := units.KiloAvax
			for i := 0; i < int(totalDuration/shortDuration); i++ {
				reward := impl.calculator.Calculate(time.Time{}, shortDuration, shortBalance, 359*units.MegaAvax+shortBalance)
				shortBalance += reward
			}
			reward := impl.calculator.Calculate(time.Time{}, totalDuration%shortDuration, shortBalance, 359*units.MegaAvax+shortBalance)
			shortBalance += reward

			longBalance := units.KiloAvax
			longBalance += impl.calculator.Calculate(time.Time{}, totalDuration, longBalance, 359*units.MegaAvax+longBalance)
			require.Less(t, shortBalance, longBalance, "should promote stakers to stake longer")
		})
	}
}

func TestRewards(t *testing.T) {
	tests := []struct {
		duration       time.Duration
		stakeAmount    uint64
		existingAmount uint64
		expectedReward uint64
	}{
		// Max duration:
		{ // (720M - 360M) * (1M / 360M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: 360 * units.MegaAvax,
			expectedReward: 120 * units.KiloAvax,
		},
		{ // (720M - 400M) * (1M / 400M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: 400 * units.MegaAvax,
			expectedReward: 96 * units.KiloAvax,
		},
		{ // (720M - 400M) * (2M / 400M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    2 * units.MegaAvax,
			existingAmount: 400 * units.MegaAvax,
			expectedReward: 192 * units.KiloAvax,
		},
		{ // (720M - 720M) * (1M / 720M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: defaultConfig.SupplyCap,
			expectedReward: 0,
		},
		// Min duration:
		// (720M - 360M) * (1M / 360M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: 360 * units.MegaAvax,
			expectedReward: 274122724713,
		},
		// (720M - 360M) * (.005 / 360M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    defaultMinValidatorStake,
			existingAmount: 360 * units.MegaAvax,
			expectedReward: 1370,
		},
		// (720M - 400M) * (1M / 400M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: 400 * units.MegaAvax,
			expectedReward: 219298179771,
		},
		// (720M - 400M) * (2M / 400M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    2 * units.MegaAvax,
			existingAmount: 400 * units.MegaAvax,
			expectedReward: 438596359542,
		},
		// (720M - 720M) * (1M / 720M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaAvax,
			existingAmount: defaultConfig.SupplyCap,
			expectedReward: 0,
		},
	}
	for _, impl := range newCalculatorsBeforeHelicon(defaultConfig) {
		t.Run(impl.name, func(t *testing.T) {
			for _, test := range tests {
				name := fmt.Sprintf("reward(%s,%d,%d)==%d",
					test.duration,
					test.stakeAmount,
					test.existingAmount,
					test.expectedReward,
				)
				t.Run(name, func(t *testing.T) {
					reward := impl.calculator.Calculate(
						time.Time{},
						test.duration,
						test.stakeAmount,
						test.existingAmount,
					)
					require.Equal(t, test.expectedReward, reward)
				})
			}
		})
	}
}

func TestPrimaryNetworkCalculatorHeliconRewards(t *testing.T) {
	const (
		heliconReductionPeriod = 90 * 24 * time.Hour
		duration               = defaultMinStakingDuration
		amount                 = units.MegaAvax
		supply                 = 360 * units.MegaAvax
	)

	heliconTime := time.Unix(1_000_000, 0)
	upgradeConfig := upgradetest.GetConfigWithUpgradeTime(upgradetest.Helicon, heliconTime)
	config := defaultConfig

	// expectedReward = (config.SupplyCap-supply) *
	// (minRate*config.MintingPeriod +
	// (config.MaxConsumptionRate-minRate)*duration) * amount * duration /
	// (config.MintingPeriod*PercentDenominator) / supply / config.MintingPeriod.
	tests := []struct {
		name               string
		minConsumptionRate uint64
		stakeStartTime     time.Time
		expectedReward     uint64
	}{
		{
			// minRate = 10% at the start of the ramp.
			name:               "at_helicon",
			minConsumptionRate: 100_000,
			stakeStartTime:     heliconTime,
			expectedReward:     274122724713,
		},
		{
			// minRate = 10% - (10% - 7.5%) / 3 = 9.1667%.
			name:               "one_third_ramp",
			minConsumptionRate: 100_000,
			stakeStartTime:     heliconTime.Add(heliconReductionPeriod / 3),
			expectedReward:     251355136048,
		},
		{
			// minRate = 7.5% at the end of the ramp.
			name:               "at_ramp_end",
			minConsumptionRate: 100_000,
			stakeStartTime:     heliconTime.Add(heliconReductionPeriod),
			expectedReward:     205817226496,
		},
		{
			// minRate remains 7.5% after the ramp.
			name:               "after_ramp",
			minConsumptionRate: 100_000,
			stakeStartTime:     heliconTime.Add(heliconReductionPeriod + time.Second),
			expectedReward:     205817226496,
		},
		{
			// minRate = 8% - (8% - 7.5%) / 2 = 7.75%.
			name:               "custom_min_rate_above_target",
			minConsumptionRate: 80_000,
			stakeStartTime:     heliconTime.Add(heliconReductionPeriod / 2),
			expectedReward:     212647776318,
		},
		{
			// minRate remains 1%, which is below the target.
			name:               "custom_min_rate_below_target",
			minConsumptionRate: 10_000,
			stakeStartTime:     heliconTime.Add(heliconReductionPeriod),
			expectedReward:     28222931131,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config
			cfg.MinConsumptionRate = tt.minConsumptionRate
			c := NewPrimaryNetworkCalculator(cfg, upgradeConfig)

			reward := c.Calculate(
				tt.stakeStartTime,
				duration,
				amount,
				supply,
			)
			require.Equal(t, tt.expectedReward, reward)
		})
	}
}

func TestRewardsOverflow(t *testing.T) {
	var (
		maxSupply     uint64 = math.MaxUint64
		initialSupply uint64 = 1
	)
	config := Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	}
	for _, impl := range newCalculatorsBeforeHelicon(config) {
		t.Run(impl.name, func(t *testing.T) {
			reward := impl.calculator.Calculate(
				time.Time{},
				defaultMinStakingDuration,
				maxSupply, // The staked amount is larger than the current supply
				initialSupply,
			)
			require.Equal(t, maxSupply-initialSupply, reward)
		})
	}
}

func TestRewardsMint(t *testing.T) {
	var (
		maxSupply     uint64 = 1000
		initialSupply uint64 = 1
	)
	config := Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	}
	for _, impl := range newCalculatorsBeforeHelicon(config) {
		t.Run(impl.name, func(t *testing.T) {
			reward := impl.calculator.Calculate(
				time.Time{},
				defaultMinStakingDuration,
				maxSupply, // The staked amount is larger than the current supply
				initialSupply,
			)
			require.Equal(t, maxSupply-initialSupply, reward)
		})
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		amount        uint64
		shares        uint32
		expectedSplit uint64
	}{
		{
			amount:        1000,
			shares:        PercentDenominator / 2,
			expectedSplit: 500,
		},
		{
			amount:        1,
			shares:        PercentDenominator,
			expectedSplit: 1,
		},
		{
			amount:        1,
			shares:        PercentDenominator - 1,
			expectedSplit: 1,
		},
		{
			amount:        1,
			shares:        1,
			expectedSplit: 1,
		},
		{
			amount:        1,
			shares:        0,
			expectedSplit: 0,
		},
		{
			amount:        9223374036974675809,
			shares:        2,
			expectedSplit: 18446748749757,
		},
		{
			amount:        9223374036974675809,
			shares:        PercentDenominator,
			expectedSplit: 9223374036974675809,
		},
		{
			amount:        9223372036855275808,
			shares:        PercentDenominator - 2,
			expectedSplit: 9223353590111202098,
		},
		{
			amount:        9223372036855275808,
			shares:        2,
			expectedSplit: 18446744349518,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d", test.amount, test.shares), func(t *testing.T) {
			require := require.New(t)

			split, remainder := Split(test.amount, test.shares)
			require.Equal(test.expectedSplit, split)
			require.Equal(test.amount-test.expectedSplit, remainder)
		})
	}
}
