// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

func TestWeightedWithoutReplacementGeneric(t *testing.T) {
	WeightedWithoutReplacementTest(t, &weightedWithoutReplacementGeneric{
		u: &uniformReplacer{},
		w: &weightedHeap{},
	})
}
