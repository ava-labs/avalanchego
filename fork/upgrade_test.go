// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestVerifyUpgradeOverride(t *testing.T) {
	defaults := upgrade.GetConfig(constants.MainnetID)
	// A fork time before the default Helicon activation, so that Helicon is a
	// "future" upgrade relative to the fork.
	forkTime := defaults.HeliconTime.Add(-time.Hour)

	tests := []struct {
		name    string
		modify  func(c *upgrade.Config)
		wantErr error
	}{
		{name: "unchanged", modify: func(*upgrade.Config) {}},
		{name: "future upgrade moved later", modify: func(c *upgrade.Config) { c.HeliconTime = c.HeliconTime.Add(24 * time.Hour) }},
		{name: "future upgrade moved to exactly T", modify: func(c *upgrade.Config) { c.HeliconTime = forkTime }},
		{name: "future upgrade moved before T", modify: func(c *upgrade.Config) { c.HeliconTime = forkTime.Add(-time.Second) }, wantErr: ErrUpgradeBeforeFork},
		{name: "past upgrade moved later", modify: func(c *upgrade.Config) { c.DurangoTime = forkTime.Add(time.Hour) }, wantErr: ErrUpgradeBeforeFork},
		{name: "non-time field changed", modify: func(c *upgrade.Config) { c.GraniteEpochDuration++ }, wantErr: ErrUpgradeParameterChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			override := defaults
			tt.modify(&override)
			err := VerifyUpgradeOverride(defaults, override, forkTime)
			require.ErrorIs(t, err, tt.wantErr, "VerifyUpgradeOverride()")
		})
	}
}
