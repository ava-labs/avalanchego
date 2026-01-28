// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package excess

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParams_AdjustExcess(t *testing.T) {
	tests := []struct {
		name    string
		params  Params
		excess  uint64
		desired uint64
		want    uint64
	}{
		{
			name: "excess equals desired",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  1000,
			desired: 1000,
			want:    1000,
		},
		{
			name: "increase within max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  1000,
			desired: 1400,
			want:    1400,
		},
		{
			name: "increase exceeds max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  1000,
			desired: 2000,
			want:    1500, // capped at excess + MaxExcessDiff
		},
		{
			name: "decrease within max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  1500,
			desired: 1200,
			want:    1200,
		},
		{
			name: "decrease exceeds max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  2000,
			desired: 500,
			want:    1500, // capped at excess - MaxExcessDiff
		},
		{
			name: "increase from zero",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  0,
			desired: 300,
			want:    300,
		},
		{
			name: "increase from zero exceeds max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  0,
			desired: 1000,
			want:    500,
		},
		{
			name: "decrease to zero",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  400,
			desired: 0,
			want:    0,
		},
		{
			name: "decrease to zero exceeds max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  1000,
			desired: 0,
			want:    500,
		},
		{
			name: "large excess increase",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  5000,
			desired: 9000,
			want:    5500,
		},
		{
			name: "large excess decrease",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess:  9000,
			desired: 5000,
			want:    8500,
		},
		{
			name: "max diff of 1",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  1,
				MaxExcess:      10000,
			},
			excess:  100,
			desired: 200,
			want:    101,
		},
		{
			name: "max diff of 0 (no change allowed)",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  0,
				MaxExcess:      10000,
			},
			excess:  100,
			desired: 200,
			want:    100,
		},
		{
			name: "very large max diff",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  math.MaxUint64,
				MaxExcess:      10000,
			},
			excess:  1000,
			desired: 5000,
			want:    5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.params.AdjustExcess(tt.excess, tt.desired)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParams_CalculateValue(t *testing.T) {
	tests := []struct {
		name   string
		params Params
		excess uint64
		want   uint64
	}{
		{
			name: "zero excess",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess: 0,
			want:   1000, // MinValue * e^0 = MinValue
		},
		{
			name: "small excess",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess: 10,
			want:   1105, // approximately 1000 * e^(10/100) = 1000 * e^0.1
		},
		{
			name: "moderate excess",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess: 100,
			want:   2718, // approximately 1000 * e^(100/100) = 1000 * e^1
		},
		{
			name: "large excess",
			params: Params{
				MinValue:       1000,
				ConversionRate: 100,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess: 200,
			want:   7388, // approximately 1000 * e^(200/100) = 1000 * e^2
		},
		{
			name: "overflow - large min value and excess",
			params: Params{
				MinValue:       math.MaxUint64 / 2,
				ConversionRate: 1,
				MaxExcessDiff:  500,
				MaxExcess:      10000,
			},
			excess: 100,
			want:   math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.params.CalculateValue(tt.excess)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParams_DesiredExcess(t *testing.T) {
	params := Params{
		MinValue:       1000,
		ConversionRate: 100,
		MaxExcessDiff:  500,
		MaxExcess:      1000,
	}

	tests := []struct {
		name         string
		desiredValue uint64
		wantExcess   uint64
	}{
		{
			name:         "value equal to min value",
			desiredValue: 1000,
			wantExcess:   0,
		},
		{
			name:         "value less than min value",
			desiredValue: 500,
			wantExcess:   0, // Cannot go below MinValue
		},
		{
			name:         "value slightly above min",
			desiredValue: 1100,
			wantExcess:   10, // approximately the excess for e^0.1
		},
		{
			name:         "value at e^1 * MinValue",
			desiredValue: 2718,
			wantExcess:   100, // excess/conversion = 1
		},
		{
			name:         "value beyond max excess",
			desiredValue: 1000000000, // Large value that requires high excess
			wantExcess:   1000,       // capped at MaxExcess since desired value is too large
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := params.DesiredExcess(tt.desiredValue)
			require.Equal(t, tt.wantExcess, got)

			calculatedValue := params.CalculateValue(got)
			if got < params.MaxExcess {
				require.GreaterOrEqual(t, calculatedValue, tt.desiredValue)
				if got > 0 {
					lowerValue := params.CalculateValue(got - 1)
					require.Less(t, lowerValue, tt.desiredValue)
				}
			} else {
				// At MaxExcess, we can't necessarily reach the desired value
				// but it should be the maximum possible
				require.Equal(t, params.MaxExcess, got)
			}
		})
	}
}

func TestParams_DesiredExcessRoundTrip(t *testing.T) {
	params := Params{
		MinValue:       1000,
		ConversionRate: 100,
		MaxExcessDiff:  500,
		MaxExcess:      500,
	}

	for excess := uint64(0); excess <= 500; excess += 50 {
		value := params.CalculateValue(excess)
		calculatedExcess := params.DesiredExcess(value)

		recalculatedValue := params.CalculateValue(calculatedExcess)
		require.GreaterOrEqual(t, recalculatedValue, value)

		// The excess should be close (within 1 due to rounding)
		diff := int64(calculatedExcess) - int64(excess)
		require.GreaterOrEqual(t, diff, int64(-1), "excess difference should be >= -1")
		require.LessOrEqual(t, diff, int64(1), "excess difference should be <= 1")
	}
}
