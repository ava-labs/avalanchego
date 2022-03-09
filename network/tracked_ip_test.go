// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrackedIP(t *testing.T) {
	assert := assert.New(t)

	ip := trackedIP{
		onStopTracking: make(chan struct{}),
	}

	assert.Equal(time.Duration(0), ip.getDelay())

	ip.increaseDelay(time.Second, time.Minute)
	assert.LessOrEqual(ip.getDelay(), 2*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	assert.LessOrEqual(ip.getDelay(), 4*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	assert.LessOrEqual(ip.getDelay(), 8*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	assert.LessOrEqual(ip.getDelay(), 16*time.Second)

	ip.increaseDelay(time.Second, time.Minute)
	assert.LessOrEqual(ip.getDelay(), 32*time.Second)

	for i := 0; i < 100; i++ {
		ip.increaseDelay(time.Second, time.Minute)
		assert.LessOrEqual(ip.getDelay(), time.Minute)
	}
	assert.GreaterOrEqual(ip.getDelay(), 45*time.Second)

	ip.stopTracking()
	<-ip.onStopTracking

	ip.stopTracking()
	<-ip.onStopTracking
}
