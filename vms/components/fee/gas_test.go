// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestT(t *testing.T) {
	tests := []struct {
		minPrice              GasPrice
		excess                Gas
		gasConversionConstant GasPrice
		expectedPrice         GasPrice
	}{
		{
			minPrice:              1,
			excess:                0,
			gasConversionConstant: 1,
			expectedPrice:         1,
		},
		{
			minPrice:              1,
			excess:                1,
			gasConversionConstant: 1,
			expectedPrice:         2,
		},
		{
			minPrice:              1,
			excess:                2,
			gasConversionConstant: 1,
			expectedPrice:         6,
		},
		{
			minPrice:              1,
			excess:                10_000,
			gasConversionConstant: 10_000,
			expectedPrice:         2,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d*e^(%d/%d)=%d", test.minPrice, test.excess, test.gasConversionConstant, test.expectedPrice), func(t *testing.T) {
			outputPrice := test.minPrice.MulExp(test.excess, test.gasConversionConstant)
			require.Equal(t, test.expectedPrice, outputPrice)
		})
	}
}
