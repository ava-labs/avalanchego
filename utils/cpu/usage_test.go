// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cpu

import (
	"testing"
	"time"
)

func TestGetSampleWeights(t *testing.T) {
	newWeight, oldWeight := getSampleWeights(2*time.Second, 3*time.Second)
	t.Fatal(newWeight, oldWeight)
}
