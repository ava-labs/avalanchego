// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
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
			r := RulesExtra{
				AvalancheRules: extras.AvalancheRules{IsHelicon: tt.isHelicon},
			}
			assert.Equalf(t, tt.want, r.MinimumGasConsumption(tt.limit), "%T.MinimumGasConsumption(%d)", r, tt.limit)
		})
	}
}

func TestRulesExtra_Warp(t *testing.T) {
	tests := []struct {
		name             string
		isDurango        bool
		addr             common.Address
		wantHasPredicate bool
		wantPrecompile   bool
		wantGasOverride  bool
	}{
		{
			name: "pre_durango",
			addr: warp.ContractAddress,
		},
		{
			name:             "durango",
			isDurango:        true,
			addr:             warp.ContractAddress,
			wantHasPredicate: true,
			wantPrecompile:   true,
			wantGasOverride:  true,
		},
		{
			name:            "durango_other_address",
			isDurango:       true,
			addr:            common.Address{1},
			wantGasOverride: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RulesExtra{
				AvalancheRules: extras.AvalancheRules{IsDurango: tt.isDurango},
			}
			assert.Equalf(t, tt.wantHasPredicate, r.HasPredicate(tt.addr), "%T.HasPredicate(%s)", r, tt.addr)
			_, ok := r.PrecompileOverride(tt.addr)
			assert.Equalf(t, tt.wantPrecompile, ok, "%T.PrecompileOverride(%s) ok", r, tt.addr)
			_, override, err := r.AccessListGas(nil)
			require.NoErrorf(t, err, "%T.AccessListGas(nil)", r)
			assert.Equalf(t, tt.wantGasOverride, override, "%T.AccessListGas(nil) override", r)
		})
	}
}
