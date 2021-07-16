// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	host1 = "127.0.0.1"
	host2 = "127.0.0.2"
	host3 = "127.0.0.3"
)

func TestNoInboundConnThrottler(t *testing.T) {
	throttler := NewInboundConnThrottler(0)
	// throttler should allow all
	for i := 0; i < 10; i++ {
		allow := throttler.Allow(host1)
		assert.True(t, allow)
	}
}

func TestInboundConnThrottler(t *testing.T) {
	cooldown := 100 * time.Millisecond
	throttlerIntf := NewInboundConnThrottler(cooldown)
	go throttlerIntf.Dispatch()

	// Allow should always return true
	// when called with a given IP for the first time
	assert.True(t, throttlerIntf.Allow(host1))
	assert.True(t, throttlerIntf.Allow(host2))
	assert.True(t, throttlerIntf.Allow(host3))

	// Shouldn't allow these IPs again until [cooldown] has passed
	assert.False(t, throttlerIntf.Allow(host1))
	assert.False(t, throttlerIntf.Allow(host2))
	assert.False(t, throttlerIntf.Allow(host3))

	// Wait for the cooldown to elapse
	time.Sleep(cooldown)

	// Wait a little longer to make sure the throttler has time to
	// clear the IPs after Dispatch wakes up
	time.Sleep(25 * time.Millisecond)

	// Allow should return true now that [cooldown] has passed
	assert.True(t, throttlerIntf.Allow(host1))
	assert.True(t, throttlerIntf.Allow(host2))
	assert.True(t, throttlerIntf.Allow(host3))

	// Shouldn't allow these IPs again until [cooldown] has passed
	assert.False(t, throttlerIntf.Allow(host1))
	assert.False(t, throttlerIntf.Allow(host2))
	assert.False(t, throttlerIntf.Allow(host3))

	// Make sure [throttler.done] isn't closed
	throttler := throttlerIntf.(*inboundConnThrottler)
	select {
	case <-throttler.done:
		t.Fatal("shouldn't be done")
	default:
	}

	throttler.Stop()

	// Make sure [throttler.done] is closed
	select {
	case _, chanOpen := <-throttler.done:
		assert.False(t, chanOpen)
	default:
		t.Fatal("should be done")
	}
}
