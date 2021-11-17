// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
)

// Test that Initialize works
func TestAdaptiveTimeoutManagerInit(t *testing.T) {
	type test struct {
		config        AdaptiveTimeoutConfig
		shouldErrWith string
	}

	tests := []test{
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 2,
				TimeoutHalflife:    5 * time.Minute,
			},
			shouldErrWith: "initial timeout < minimum timeout",
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     5 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 2,
				TimeoutHalflife:    5 * time.Minute,
			},
			shouldErrWith: "initial timeout > maximum timeout",
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 0.9,
				TimeoutHalflife:    5 * time.Minute,
			},
			shouldErrWith: "timeout coefficient < 1",
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 1,
			},
			shouldErrWith: "timeout halflife is 0",
		},
		{
			config: AdaptiveTimeoutConfig{
				InitialTimeout:     2 * time.Second,
				MinimumTimeout:     2 * time.Second,
				MaximumTimeout:     3 * time.Second,
				TimeoutCoefficient: 1,
				TimeoutHalflife:    -1 * time.Second,
			},
			shouldErrWith: "timeout halflife is negative",
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
		tm := AdaptiveTimeoutManager{}
		err := tm.Initialize(&test.config, "", prometheus.NewRegistry())
		if err != nil && test.shouldErrWith == "" {
			assert.FailNow(t, "error from valid config", err)
		} else if err == nil && test.shouldErrWith != "" {
			assert.FailNowf(t, "should have errored", test.shouldErrWith)
		}
	}
}

func TestAdaptiveTimeoutManager(t *testing.T) {
	tm := AdaptiveTimeoutManager{}
	err := tm.Initialize(
		&AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     time.Hour,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		"",
		prometheus.NewRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
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
			tm.Put(ids.ID{byte(numSuccessful)}, message.PullQuery, *callback)
		}
		if numSuccessful >= 0 {
			wg.Done()
		}
		if numSuccessful%2 == 0 {
			tm.Remove(ids.ID{byte(numSuccessful)})
			tm.Put(ids.ID{byte(numSuccessful)}, message.PullQuery, *callback)
		}
	}
	(*callback)()
	(*callback)()

	wg.Wait()
}
