// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidDefaultUpgrades(t *testing.T) {
	for _, upgradeTest := range []struct {
		name    string
		upgrade Config
	}{
		{
			name:    "Default",
			upgrade: Default,
		},
		{
			name:    "Fuji",
			upgrade: Fuji,
		},
		{
			name:    "Mainnet",
			upgrade: Mainnet,
		},
	} {
		t.Run(upgradeTest.name, func(t *testing.T) {
			require := require.New(t)
			require.NoError(upgradeTest.upgrade.Validate())
		})
	}
}

func TestInvalidUpgrade(t *testing.T) {
	firstUpgradeTime := time.Now()
	invalidSecondUpgradeTime := firstUpgradeTime.Add(-1 * time.Second)
	upgrade := Config{
		ApricotPhase1Time: firstUpgradeTime,
		ApricotPhase2Time: invalidSecondUpgradeTime,
	}
	err := upgrade.Validate()
	require.ErrorIs(t, err, ErrInvalidUpgradeTimes)
}
