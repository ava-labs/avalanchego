// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func TestScalingFromTimeToDouble(t *testing.T) {
	tests := []struct {
		timeToDouble uint64
		want         gas.Gas
	}{
		{timeToDouble: 0, want: 87},
		{timeToDouble: 1, want: 1},
		{timeToDouble: 2, want: 3},
		{timeToDouble: 60, want: 87},
		{timeToDouble: 96, want: 138},
		{timeToDouble: 157, want: 227},
		{timeToDouble: 349, want: 504},
		{timeToDouble: 794, want: 1_145},
		{timeToDouble: 1_526_223_088_619_171_207, want: 2_201_874_481_241_115_226},
		{timeToDouble: 12_786_308_645_202_655_658, want: math.MaxUint64 - 2},
		{timeToDouble: 12_786_308_645_202_655_659, want: math.MaxUint64},
		{timeToDouble: 12_786_308_645_202_655_660, want: math.MaxUint64},
		{timeToDouble: math.MaxUint64, want: math.MaxUint64},
	}

	for _, test := range tests {
		require.Equal(
			t,
			test.want,
			scalingFromTimeToDouble(test.timeToDouble),
			"scalingFromTimeToDouble(%d)",
			test.timeToDouble,
		)
	}
}
