// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"errors"

	"github.com/MetalBlockchain/metalgo/utils/math"
	"github.com/MetalBlockchain/metalgo/utils/sampler"
	"github.com/MetalBlockchain/metalgo/utils/set"
)

var errUnexpectedSamplerFailure = errors.New("unexpected sampler failure")

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
		totalWeight, err = math.Add(totalWeight, weight)
		if err != nil {
			return nil, err
		}
		i++
	}

	sampler := sampler.NewWeightedWithoutReplacement()
	if err := sampler.Initialize(weights); err != nil {
		return nil, err
	}

	maxSize = int(min(uint64(maxSize), totalWeight))
	indices, ok := sampler.Sample(maxSize)
	if !ok {
		return nil, errUnexpectedSamplerFailure
	}

	sampledElements := set.NewSet[T](maxSize)
	for _, index := range indices {
		sampledElements.Add(keys[index])
	}
	return sampledElements, nil
}
