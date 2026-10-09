// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestCalculateFee(t *testing.T) {
	tests := []struct {
		name    string
		gas     gas.Gas
		price   gas.Price
		want    uint64
		wantErr error
	}{
		{
			name:  "exact",
			gas:   10_000,
			price: 25 * tx.X2CRate,
			want:  250_000,
		},
		{
			name:  "rounds_up_below_one_nAVAX",
			gas:   3,
			price: 1,
			want:  1,
		},
		{
			name:  "rounds_up_remainder",
			gas:   11_230,
			price: 25*tx.X2CRate + 1,
			want:  280_751, // 11_230 * (25e9 + 1) / 1e9 = 280_750.00001123, rounded up
		},
		{
			name:  "max_cost",
			gas:   1,
			price: math.MaxUint64,
			want:  18_446_744_074, // MaxUint64 / 1e9, rounded up
		},
		{
			name:    "cost_overflow",
			gas:     2,
			price:   math.MaxUint64,
			wantErr: safemath.ErrOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculateFee(test.gas, test.price)
			require.ErrorIsf(t, err, test.wantErr, "calculateFee(%d, %d)", test.gas, test.price)
			require.Equalf(t, test.want, got, "calculateFee(%d, %d)", test.gas, test.price)
		})
	}
}
