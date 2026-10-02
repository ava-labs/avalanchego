// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckForkOrder(t *testing.T) {
	ptr := func(v uint64) *uint64 { return &v }
	// throughEtna schedules every upgrade up to and including Etna at 1.
	throughEtna := func() NetworkUpgrades {
		var n NetworkUpgrades
		for _, f := range []**uint64{
			&n.ApricotPhase1BlockTimestamp, &n.ApricotPhase2BlockTimestamp, &n.ApricotPhase3BlockTimestamp,
			&n.ApricotPhase4BlockTimestamp, &n.ApricotPhase5BlockTimestamp, &n.ApricotPhasePre6BlockTimestamp,
			&n.ApricotPhase6BlockTimestamp, &n.ApricotPhasePost6BlockTimestamp, &n.BanffBlockTimestamp,
			&n.CortinaBlockTimestamp, &n.DurangoBlockTimestamp, &n.EtnaTimestamp,
		} {
			*f = ptr(1)
		}
		return n
	}
	// withGranite schedules Fortuna at `fortuna` and Granite at 2.
	withGranite := func(n NetworkUpgrades, fortuna *uint64) NetworkUpgrades {
		n.FortunaTimestamp = fortuna
		n.GraniteTimestamp = ptr(2)
		return n
	}
	tests := []struct {
		name     string
		upgrades NetworkUpgrades
		optional []Upgrade
		wantErr  error
	}{
		{
			name: "none_scheduled",
		},
		{
			name: "in_order",
			upgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp: ptr(0),
				ApricotPhase2BlockTimestamp: ptr(2),
			},
		},
		{
			name: "out_of_order",
			upgrades: NetworkUpgrades{
				ApricotPhase1BlockTimestamp: ptr(1),
				ApricotPhase2BlockTimestamp: ptr(0),
			},
			wantErr: ErrUnsupportedForkOrdering,
		},
		{
			name: "gap",
			upgrades: NetworkUpgrades{
				ApricotPhase2BlockTimestamp: ptr(0),
			},
			wantErr: ErrUnsupportedForkOrdering,
		},
		{
			name:     "required_unscheduled",
			upgrades: withGranite(throughEtna(), nil),
			wantErr:  ErrUnsupportedForkOrdering,
		},
		{
			name:     "optional_unscheduled",
			upgrades: withGranite(throughEtna(), nil),
			optional: []Upgrade{Fortuna},
		},
		{
			name:     "optional_scheduled_out_of_order",
			upgrades: withGranite(throughEtna(), ptr(0)),
			optional: []Upgrade{Fortuna},
			wantErr:  ErrUnsupportedForkOrdering,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.upgrades.CheckForkOrder(tt.optional...), tt.wantErr)
		})
	}
}
