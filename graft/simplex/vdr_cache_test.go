// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/simplex/avalanchego"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fetchCountingChain answers every height with validatorSetForHeight and counts the lookups.
// The embedded nil PlatformChain makes any other method panic, as none should be reached.
type fetchCountingChain struct {
	PlatformChain
	fetches atomic.Int64
	err     error
}

func (c *fetchCountingChain) GetValidatorSet(height uint64) (metadata.NodeBLSMappings, error) {
	c.fetches.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return validatorSetForHeight(height), nil
}

func validatorSetForHeight(height uint64) metadata.NodeBLSMappings {
	return metadata.NodeBLSMappings{{NodeID: avalanchego.NodeID{byte(height)}, BLSKey: []byte{byte(height)}, Weight: height}}
}

func newTestValidatorCache() (*ValidatorCache, *fetchCountingChain) {
	pc := &fetchCountingChain{}
	return &ValidatorCache{PlatformChain: pc}, pc
}

// requireFetch asserts the lookup of height returned its set and left the fetch count at want.
func requireFetch(t *testing.T, vc *ValidatorCache, pc *fetchCountingChain, height uint64, want int64) {
	set, err := vc.GetValidatorSet(height)
	require.NoError(t, err)
	require.Equal(t, validatorSetForHeight(height), set)
	require.EqualValues(t, want, pc.fetches.Load())
}

func TestValidatorCacheHitsAndMisses(t *testing.T) {
	vc, pc := newTestValidatorCache()

	// Height 0 collides with the zero value of the cached height, so it must be fetched
	// the first time and served from the cache afterwards like any other height.
	requireFetch(t, vc, pc, 0, 1)
	requireFetch(t, vc, pc, 0, 1)

	// A different height is a miss that replaces the entry; the old height then misses again.
	requireFetch(t, vc, pc, 7, 2)
	requireFetch(t, vc, pc, 7, 2)
	requireFetch(t, vc, pc, 0, 3)
}

func TestValidatorCacheErrors(t *testing.T) {
	vc, pc := newTestValidatorCache()
	requireFetch(t, vc, pc, 1, 1)

	// A failed lookup is returned and neither cached nor allowed to disturb the existing entry.
	pc.err = errors.New("p-chain unavailable")
	_, err := vc.GetValidatorSet(2)
	require.ErrorIs(t, err, pc.err)
	pc.err = nil

	requireFetch(t, vc, pc, 1, 2)
	requireFetch(t, vc, pc, 2, 3)
}

func TestValidatorCacheReturnedSliceIsIsolated(t *testing.T) {
	vc, pc := newTestValidatorCache()

	// A caller that mutates what it was handed must not change what later callers see,
	// whether it was handed the result of a miss or of a hit.
	for _, path := range []string{"miss", "hit"} {
		set, err := vc.GetValidatorSet(4)
		require.NoError(t, err)
		set[0].Weight = 12345
		set[0].NodeID = avalanchego.NodeID{0xFF}

		again, err := vc.GetValidatorSet(4)
		require.NoError(t, err)
		require.Equal(t, validatorSetForHeight(4), again, "mutating the %s result leaked into the cache", path)
	}
	require.EqualValues(t, 1, pc.fetches.Load())
}

func TestValidatorCacheConcurrentAccessReturnsRequestedHeight(t *testing.T) {
	vc, _ := newTestValidatorCache()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Go(func() {
			for i := 0; i < 2000; i++ {
				height := uint64(1 + (g+i)%2)
				set, err := vc.GetValidatorSet(height)
				// If the cached height and result are not updated together a reader
				// can be handed the set of the other height. assert, not require:
				// FailNow must not be called from a non-test goroutine.
				assert.NoError(t, err)
				assert.Equal(t, validatorSetForHeight(height), set)
			}
		})
	}
	wg.Wait()
}
