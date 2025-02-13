// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// Test that Initialize works
func TestAdaptiveTimeoutManagerInit(t *testing.T) {
	type test struct {
		config      AdaptiveTimeoutConfig
		expectedErr error
	}

	tests := []*test{
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 2,
				TimeoutHalflife:    5 * time.Minute,
			},
			expectedErr: errInitialTimeoutBelowMinimum,
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     5 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 2,
				TimeoutHalflife:    5 * time.Minute,
			},
			expectedErr: errInitialTimeoutAboveMaximum,
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 0.9,
				TimeoutHalflife:    5 * time.Minute,
			},
			expectedErr: errTooSmallTimeoutCoefficient,
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 1,
			},
			expectedErr: errNonPositiveHalflife,
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 1,
				TimeoutHalflife:    -1 * time.Second,
			},
			expectedErr: errNonPositiveHalflife,
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 1,
				TimeoutHalflife:    5 * time.Minute,
			},
		},
	}

	for _, test := range tests {
		_, err := NewAdaptiveTimeoutManager(&test.config, prometheus.NewRegistry())
		require.ErrorIs(t, err, test.expectedErr)
	}
}

func TestAdaptiveTimeoutManager(t *testing.T) {
	tm, err := NewAdaptiveTimeoutManager(
		&AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     time.Hour,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	go tm.Dispatch()

	var lock sync.Mutex

	numSuccessful := 5

	wg := sync.WaitGroup{}
	wg.Add(numSuccessful)

	callback := new(func())
	*callback = func() {
		lock.Lock()
		defer lock.Unlock()

		numSuccessful--
		if numSuccessful > 0 {
			tm.Put(ids.RequestID{Op: byte(numSuccessful)}, true, *callback)
		}
		if numSuccessful >= 0 {
			wg.Done()
		}
		if numSuccessful%2 == 0 {
			tm.Remove(ids.RequestID{Op: byte(numSuccessful)})
			tm.Put(ids.RequestID{Op: byte(numSuccessful)}, true, *callback)
		}
	}
	(*callback)()
	(*callback)()

	wg.Wait()
}
