// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const epsilon = 1e-9

func TestGetSampleWeights(t *testing.T) {
	tests := []struct {
		name      string
		frequency time.Duration
		halflife  time.Duration
		oldWeight float64
	}{
		{
			name:      "simple equal values",
			frequency: 2 * time.Second,
			halflife:  2 * time.Second,
			oldWeight: .5,
		},
		{
			name:      "two periods values",
			frequency: 2 * time.Second,
			halflife:  4 * time.Second,
			oldWeight: math.Sqrt(.5),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert := assert.New(t)

			newWeight, oldWeight := getSampleWeights(test.frequency, test.halflife)
			assert.InDelta(1-test.oldWeight, newWeight, epsilon)
			assert.InDelta(test.oldWeight, oldWeight, epsilon)
		})
	}
}
