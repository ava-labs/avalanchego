// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Sample keys from [elements] uniformly by weight without replacement. The
// returned set will have size less than or equal to [maxSize]. This function
// will error if the sum of all weights overflows.
func Sample[T comparable](elements map[T]uint64, maxSize int) (set.Set[T], error) {
	var (
		keys        = make([]T, len(elements))
		weights     = make([]uint64, len(elements))
		totalWeight uint64
		err         error
	)
	i := 0
	for key, weight := range elements {
		keys[i] = key
		weights[i] = weight
		totalWeight, err = math.Add64(totalWeight, weight)
		if err != nil {
			return nil, err
		}
		i++
	}

	sampler := sampler.NewWeightedWithoutReplacement()
	if err := sampler.Initialize(weights); err != nil {
		return nil, err
	}

	maxSize = int(math.Min(uint64(maxSize), totalWeight))
	indices, err := sampler.Sample(maxSize)
	if err != nil {
		return nil, err
	}

	sampledElements := set.NewSet[T](maxSize)
	for _, index := range indices {
		sampledElements.Add(keys[index])
	}
	return sampledElements, nil
}
