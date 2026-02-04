// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestLongerDurationBonus(t *testing.T) {
	c := NewCalculator(defaultConfig)
	shortDuration := 24 * time.Hour
	totalDuration := 365 * 24 * time.Hour
	shortBalance := units.KiloAvax
	for i := 0; i < int(totalDuration/shortDuration); i++ {
		r := c.Calculate(shortDuration, shortBalance, 359*units.MegaAvax+shortBalance)
		shortBalance += r
	}
	reward := c.Calculate(totalDuration%shortDuration, shortBalance, 359*units.MegaAvax+shortBalance)
	shortBalance += reward

	longBalance := units.KiloAvax
	longBalance += c.Calculate(totalDuration, longBalance, 359*units.MegaAvax+longBalance)
	require.Less(t, shortBalance, longBalance, "should promote stakers to stake longer")
}

func TestRewards(t *testing.T) {
	c := NewCalculator(defaultConfig)
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
	for _, test := range tests {
		name := fmt.Sprintf("reward(%s,%d,%d)==%d",
			test.duration,
			test.stakeAmount,
			test.existingAmount,
			test.expectedReward,
		)
		t.Run(name, func(t *testing.T) {
			reward := c.Calculate(
				test.duration,
				test.stakeAmount,
				test.existingAmount,
			)
			require.Equal(t, test.expectedReward, reward)
		})
	}
}

func TestRewardsOverflow(t *testing.T) {
	var (
		maxSupply     uint64 = math.MaxUint64
		initialSupply uint64 = 1
	)
	c := NewCalculator(Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	})
	reward := c.Calculate(
		defaultMinStakingDuration,
		maxSupply, // The staked amount is larger than the current supply
		initialSupply,
	)
	require.Equal(t, maxSupply-initialSupply, reward)
}

func TestRewardsMint(t *testing.T) {
	var (
		maxSupply     uint64 = 1000
		initialSupply uint64 = 1
	)
	c := NewCalculator(Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	})
	rewards := c.Calculate(
		defaultMinStakingDuration,
		maxSupply, // The staked amount is larger than the current supply
		initialSupply,
	)
	require.Equal(t, maxSupply-initialSupply, rewards)
}

func TestProportionalAmount(t *testing.T) {
	tests := []struct {
		name        string
		amount      uint64
		numerator   uint64
		denominator uint64
		expected    uint64
		expectedErr bool
	}{
		{
			name:        "division by zero",
			amount:      100,
			numerator:   5,
			denominator: 0,
			expectedErr: true,
		},
		{
			name:        "basic case 1",
			amount:      100,
			numerator:   3,
			denominator: 10,
			expected:    30,
		},
		{
			name:        "basic case 2",
			amount:      250,
			numerator:   4,
			denominator: 50,
			expected:    20,
		},
		{
			name:        "precision",
			amount:      7,
			numerator:   3,
			denominator: 10,
			expected:    2,
		},
		{
			name:        "overflow",
			amount:      math.MaxUint64,
			numerator:   2,
			denominator: 10,
			expected:    math.MaxUint64/5 - 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			result, err := ProportionalAmount(test.amount, test.numerator, test.denominator)
			if test.expectedErr {
				require.Error(err) //nolint:forbidigo
				return
			}
			require.NoError(err)
			require.Equal(test.expected, result)
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
