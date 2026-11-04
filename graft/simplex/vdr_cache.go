// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"sync"

	metadata "github.com/ava-labs/simplex/msm"
)

// ValidatorCache wraps a PlatformChain and remembers the validator set of the
// most recently requested P-chain height, so repeated lookups of the same
// height do not reach the P-chain.
type ValidatorCache struct {
	PlatformChain
	lock         sync.Mutex
	cachedHeight uint64
	cachedResult metadata.NodeBLSMappings
}

func (vc *ValidatorCache) GetValidatorSet(height uint64) (metadata.NodeBLSMappings, error) {
	vc.lock.Lock()
	defer vc.lock.Unlock()
	cachedHeight := vc.cachedHeight
	cachedResult := vc.cachedResult

	if height == cachedHeight && len(cachedResult) != 0 {
		return cachedResult.Clone(), nil
	}

	// Check again in case another goroutine updated the cache while we were waiting for the lock.
	if height == vc.cachedHeight && len(vc.cachedResult) != 0 {
		return vc.cachedResult.Clone(), nil
	}

	result, err := vc.PlatformChain.GetValidatorSet(height)
	if err != nil {
		return nil, err
	}

	vc.cachedHeight = height
	vc.cachedResult = result

	return result.Clone(), nil
}
