// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
)

func TestRulesExtra_MinimumGasConsumption(t *testing.T) {
	tests := []struct {
		name      string
		isHelicon bool
		limit     uint64
		want      uint64
	}{
		{"pre_fork_noop", false, math.MaxUint64, 0},
		{"post_fork_zero", true, 0, 0},
		{"post_fork_odd_rounds_up", true, 3, 2},
		{"post_fork_tx_gas", true, 21_000, 10_500},
		{"post_fork_max_no_overflow", true, math.MaxUint64, 1 << 63},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesExtra(extras.Rules{
				AvalancheRules: extras.AvalancheRules{IsHelicon: tt.isHelicon},
			})
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestPredicateBytesFromExtra(t *testing.T) {
	tests := []struct {
		name  string
		rules extras.AvalancheRules
		extra []byte
		want  []byte
	}{
		{
			name: "empty_extra",
		},
		{
			name:  "too_short",
			extra: make([]byte, ap3WindowSize-1),
		},
		{
			name:  "empty_predicate",
			extra: make([]byte, ap3WindowSize),
		},
		{
			name: "non_empty_predicate",
			extra: []byte{
				ap3WindowSize: 5,
			},
			want: []byte{5},
		},
		{
			name: "fortuna_empty_extra",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
		},
		{
			name: "fortuna_too_short",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: make([]byte, ACP176StateSize-1),
		},
		{
			name: "fortuna_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: make([]byte, ACP176StateSize),
		},
		{
			name: "fortuna_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsFortuna: true,
			},
			extra: []byte{
				ACP176StateSize: 5,
			},
			want: []byte{5},
		},
		{
			name: "helicon_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
		},
		{
			name: "helicon_non_empty_predicate",
			rules: extras.AvalancheRules{
				IsHelicon: true,
			},
			extra: []byte{5},
			want:  []byte{5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := predicateBytesFromExtra(tt.rules, tt.extra)
			require.Equal(t, tt.want, got, "predicateBytesFromExtra()")
		})
	}
}
