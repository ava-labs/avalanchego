// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"math"
	"testing"

	"github.com/kylelemons/godebug/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade"
)

func TestRules_MinimumGasConsumption(t *testing.T) {
	tests := []struct {
		name   string
		latest upgrade.Upgrade
		limit  uint64
		want   uint64
	}{
		{"pre_fork_noop", upgrade.Granite, math.MaxUint64, 0},
		{"post_fork_zero", upgrade.Helicon, 0, 0},
		{"post_fork_odd_rounds_up", upgrade.Helicon, 3, 2},
		{"post_fork_tx_gas", upgrade.Helicon, 21_000, 10_500},
		{"post_fork_max_no_overflow", upgrade.Helicon, math.MaxUint64, 1 << 63},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Rules{LatestUpgrade: tt.latest}
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestLatestUpgradeFromConfig(t *testing.T) {
	const (
		no  = upgrade.NoUpgrades
		ap1 = upgrade.ApricotPhase1
		ap2 = upgrade.ApricotPhase2
		ap3 = upgrade.ApricotPhase3
		ap4 = upgrade.ApricotPhase4
	)

	p := func(u uint64) *uint64 { return &u }
	shared := p(30)
	config := &ChainConfig{
		ApricotPhase1Timestamp: p(10),
		ApricotPhase2Timestamp: shared,
		ApricotPhase3Timestamp: shared,
		ApricotPhase4Timestamp: p(40),
	}
	tests := map[uint64]upgrade.Upgrade{
		// Dawn of time
		0: no,
		1: no,
		// Around AP1
		9:  no,
		10: ap1,
		11: ap1,
		// Return latest when times are shared
		*shared: ap3,
		// Around AP4
		39: ap3,
		40: ap4,
		41: ap4,
		// Heat death of the universe
		math.MaxUint64: ap4,
	}

	t.Logf("%T:\n%s", config, pretty.Sprint(config))
	require.NoErrorf(t, config.CheckConfigForkOrder(), "%T.CheckConfigForkOrder()", config)

	for timestamp, want := range tests {
		if got := config.Rules(timestamp).LatestUpgrade; got != want {
			t.Errorf("%T.Rules(%d).LatestUpgrade = %s; want %s", config, timestamp, got, want)
		}
	}
}
