// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/params"
)

func TestCalculateDynamicFee(t *testing.T) {
	type test struct {
		gas           uint64
		baseFee       *big.Int
		expectedErr   error
		expectedValue uint64
	}
	var tests []test = []test{
		{
			gas:           1,
			baseFee:       new(big.Int).Set(x2cRate),
			expectedValue: 1,
		},
		{
			gas:           21000,
			baseFee:       big.NewInt(25 * params.GWei),
			expectedValue: 525000,
		},
	}

	for _, test := range tests {
		cost, err := calculateDynamicFee(test.gas, test.baseFee)
		if test.expectedErr == nil {
			if err != nil {
				t.Fatalf("Unexpectedly failed to calculate dynamic fee: %s", err)
			}
			if cost != test.expectedValue {
				t.Fatalf("Expected value: %d, found: %d", test.expectedValue, cost)
			}
		} else {
			if err != test.expectedErr {
				t.Fatalf("Expected error: %s, found error: %s", test.expectedErr, err)
			}
		}
	}
}
