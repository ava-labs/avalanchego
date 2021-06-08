// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	host1 = "127.0.0.1:9651"
	host2 = "127.0.0.1:9653"
	host3 = "127.0.0.1:9655"
)

func TestNoConnMeter(t *testing.T) {
	meters := []ConnMeter{
		NewConnMeter(0, 1, 1),
		NewConnMeter(time.Nanosecond, 0, 1),
		NewConnMeter(time.Nanosecond, 1, 0),
	}
	// Each meter should allow all
	for _, meter := range meters {
		for i := 0; i < 10; i++ {
			allow := meter.Allow(host1)
			assert.True(t, allow)
		}
	}
}

// Test that we allow <= [maxConns] per [resetDuration]
func TestConnMeterMaxConnsMet(t *testing.T) {
	meter := NewConnMeter(time.Hour, 1, 3)
	for i := 0; i < 3; i++ {
		allow := meter.Allow(host1)
		assert.True(t, allow)
	}
	allow := meter.Allow(host1)
	assert.False(t, allow)
}

// Test that old connections are dropped from the connection
// counter. This test assumes that calling Allow 3 times takes
// less than 250 ms.
func TestConnMeterOldConnsCleared(t *testing.T) {
	meter := NewConnMeter(250*time.Millisecond, 1, 3)

	// Allow 3 within 250 ms
	for i := 0; i < 3; i++ {
		allow := meter.Allow(host1)
		assert.True(t, allow)
	}
	// 4th isn't allowed
	allow := meter.Allow(host1)
	assert.False(t, allow)

	// Sleep 250 ms. Connections should be cleared.
	time.Sleep(250 * time.Millisecond)
	// Allow 3 within 250 ms
	for i := 0; i < 3; i++ {
		allow := meter.Allow(host1)
		assert.True(t, allow)
	}
	// 4th isn't allowed
	allow = meter.Allow(host1)
	assert.False(t, allow)
}

// Make sure that allowed connection attempts
// from one host aren't counted against another host.
func TestConnMeterMultipleHosts(t *testing.T) {
	m := NewConnMeter(time.Hour, 5, 1)

	allow := m.Allow(host1)
	assert.True(t, allow)
	allow = m.Allow(host1)
	assert.False(t, allow)

	allow = m.Allow(host2)
	assert.True(t, allow)
	allow = m.Allow(host2)
	assert.False(t, allow)

	allow = m.Allow(host3)
	assert.True(t, allow)
	allow = m.Allow(host3)
	assert.False(t, allow)
}

// Test that the connection counter cache size
// is being set properly
func TestConnMeterCacheSize(t *testing.T) {
	m := NewConnMeter(time.Hour, 1, 1)

	allow := m.Allow(host1)
	assert.True(t, allow)
	allow = m.Allow(host1)
	assert.False(t, allow) // Rate-limited

	// Should kick host 1 out of the cache
	allow = m.Allow(host2)
	assert.True(t, allow)

	allow = m.Allow(host1)
	assert.True(t, allow) // No longer rate-limited
}
