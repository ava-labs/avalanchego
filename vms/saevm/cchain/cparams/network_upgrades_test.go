// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestIsTimestampForked(t *testing.T) {
	type test struct {
		fork     *uint64
		block    uint64
		isForked bool
	}

	for name, test := range map[string]test{
		"nil fork at 0": {
			fork:     nil,
			block:    0,
			isForked: false,
		},
		"nil fork at non-zero": {
			fork:     nil,
			block:    100,
			isForked: false,
		},
		"zero fork at genesis": {
			fork:     utils.PointerTo[uint64](0),
			block:    0,
			isForked: true,
		},
		"pre fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
			block:    50,
			isForked: false,
		},
		"at fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
			block:    100,
			isForked: true,
		},
		"post fork timestamp": {
			fork:     utils.PointerTo[uint64](100),
			block:    150,
			isForked: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := isTimestampForked(test.fork, test.block)
			require.Equal(t, test.isForked, res)
		})
	}
}

// TestForks ensures that every upgrade is included in [NetworkUpgrades.forks],
// in declaration order, as it drives fork ordering and compatibility checks.
func TestForks(t *testing.T) {
	var n NetworkUpgrades
	v := reflect.ValueOf(&n).Elem()
	for i := range v.NumField() {
		v.Field(i).Set(reflect.ValueOf(utils.PointerTo(uint64(i))))
	}

	forks := n.forks()
	require.Len(t, forks, v.NumField())
	for i, f := range forks {
		require.Samef(t, v.Field(i).Interface(), f.timestamp, "fork %q timestamp", f.name)
	}
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		set  func(*NetworkUpgrades, *uint64)
		want AvalancheRules
	}{
		{
			name: "ApricotPhase1",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase1Timestamp = t },
			want: AvalancheRules{IsApricotPhase1: true},
		},
		{
			name: "ApricotPhase2",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase2Timestamp = t },
			want: AvalancheRules{IsApricotPhase2: true},
		},
		{
			name: "ApricotPhase3",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase3Timestamp = t },
		},
		{
			name: "ApricotPhase4",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase4Timestamp = t },
		},
		{
			name: "ApricotPhase5",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase5Timestamp = t },
		},
		{
			name: "ApricotPhasePre6",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhasePre6Timestamp = t },
			want: AvalancheRules{IsApricotPhasePre6: true},
		},
		{
			name: "ApricotPhase6",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhase6Timestamp = t },
			want: AvalancheRules{IsApricotPhase6: true},
		},
		{
			name: "ApricotPhasePost6",
			set:  func(n *NetworkUpgrades, t *uint64) { n.ApricotPhasePost6Timestamp = t },
		},
		{
			name: "Banff",
			set:  func(n *NetworkUpgrades, t *uint64) { n.BanffTimestamp = t },
			want: AvalancheRules{IsBanff: true},
		},
		{
			name: "Cortina",
			set:  func(n *NetworkUpgrades, t *uint64) { n.CortinaTimestamp = t },
		},
		{
			name: "Durango",
			set:  func(n *NetworkUpgrades, t *uint64) { n.DurangoTimestamp = t },
			want: AvalancheRules{IsDurango: true},
		},
		{
			name: "Etna",
			set:  func(n *NetworkUpgrades, t *uint64) { n.EtnaTimestamp = t },
		},
		{
			name: "Fortuna",
			set:  func(n *NetworkUpgrades, t *uint64) { n.FortunaTimestamp = t },
			want: AvalancheRules{IsFortuna: true},
		},
		{
			name: "Granite",
			set:  func(n *NetworkUpgrades, t *uint64) { n.GraniteTimestamp = t },
			want: AvalancheRules{IsGranite: true},
		},
		{
			name: "Helicon",
			set:  func(n *NetworkUpgrades, t *uint64) { n.HeliconTimestamp = t },
			want: AvalancheRules{IsHelicon: true},
		},
		{
			name: "Igloo",
			set:  func(n *NetworkUpgrades, t *uint64) { n.IglooTimestamp = t },
			want: AvalancheRules{IsIgloo: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const activation = 100
			var n NetworkUpgrades
			tt.set(&n, utils.PointerTo[uint64](activation))

			require.Zero(t, n.Rules(activation-1), "Rules() before activation")
			require.Equal(t, tt.want, n.Rules(activation), "Rules() at activation")
		})
	}
}

func TestCheckUpgradesCompatible(t *testing.T) {
	tests := []struct {
		name    string
		stored  NetworkUpgrades
		new     NetworkUpgrades
		head    uint64
		wantErr *ethparams.ConfigCompatError
	}{
		{
			name:   "identical",
			stored: NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](50)},
			new:    NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](50)},
			head:   100,
		},
		{
			name:   "future_fork_rescheduled",
			stored: NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](200)},
			new:    NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](300)},
			head:   100,
		},
		{
			name:   "future_fork_unscheduled",
			stored: NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](200)},
			head:   100,
		},
		{
			name:   "active_fork_rescheduled",
			stored: NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](50)},
			new:    NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](60)},
			head:   100,
			wantErr: &ethparams.ConfigCompatError{
				What:         "Durango fork block timestamp",
				StoredTime:   utils.PointerTo[uint64](50),
				NewTime:      utils.PointerTo[uint64](60),
				RewindToTime: 49,
			},
		},
		{
			name:   "active_fork_unscheduled_at_genesis",
			stored: NetworkUpgrades{ApricotPhase5Timestamp: utils.PointerTo[uint64](0)},
			head:   0,
			wantErr: &ethparams.ConfigCompatError{
				What:       "Apricot Phase 5 fork block timestamp",
				StoredTime: utils.PointerTo[uint64](0),
			},
		},
		{
			name:   "active_fork_unscheduled",
			stored: NetworkUpgrades{ApricotPhase5Timestamp: utils.PointerTo[uint64](0)},
			head:   100,
			wantErr: &ethparams.ConfigCompatError{
				What:       "Apricot Phase 5 fork block timestamp",
				StoredTime: utils.PointerTo[uint64](0),
			},
		},
		{
			name: "fork_scheduled_in_past",
			new:  NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](50)},
			head: 100,
			wantErr: &ethparams.ConfigCompatError{
				What:         "Durango fork block timestamp",
				NewTime:      utils.PointerTo[uint64](50),
				RewindToTime: 49,
			},
		},
		{
			name: "fork_scheduled_at_head",
			new:  NetworkUpgrades{DurangoTimestamp: utils.PointerTo[uint64](100)},
			head: 100,
			wantErr: &ethparams.ConfigCompatError{
				What:         "Durango fork block timestamp",
				NewTime:      utils.PointerTo[uint64](100),
				RewindToTime: 99,
			},
		},
		{
			name: "first_incompatible_fork_reported",
			stored: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](10),
				DurangoTimestamp:       utils.PointerTo[uint64](10),
			},
			new: NetworkUpgrades{
				ApricotPhase1Timestamp: utils.PointerTo[uint64](20),
				DurangoTimestamp:       utils.PointerTo[uint64](20),
			},
			head: 100,
			wantErr: &ethparams.ConfigCompatError{
				What:         "Apricot Phase 1 fork block timestamp",
				StoredTime:   utils.PointerTo[uint64](10),
				NewTime:      utils.PointerTo[uint64](20),
				RewindToTime: 9,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stored.checkUpgradesCompatible(&tt.new, tt.head)
			require.Equal(t, tt.wantErr, got)
		})
	}
}
