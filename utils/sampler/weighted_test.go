// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	tests = []func(*testing.T, Weighted){
		InitializeOverflowTest,
		OutOfRangeTest,
		SingletonTest,
		WithZeroTest,
	}
)

func WeightedTest(t *testing.T, s Weighted) {
	for _, test := range tests {
		test(t, s)
	}
}

func InitializeOverflowTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1, math.MaxUint64})
	assert.Error(t, err, "should have reported an overflow error")
}

func OutOfRangeTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	_, err = s.Sample(1)
	assert.Error(t, err, "should have reported an out of range error")
}

func SingletonTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	index, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Equal(t, 0, index, "should have selected the first element")
}

func WithZeroTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{0, 1})
	assert.NoError(t, err)

	index, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Equal(t, 1, index, "should have selected the second element")
}
