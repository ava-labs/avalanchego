// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

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
