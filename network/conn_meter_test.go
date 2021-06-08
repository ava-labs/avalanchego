// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	localhost = "127.0.0.1:9651"
)

func TestNoConnMeter(t *testing.T) {
	m := NewConnMeter(0, 1, 1)

	allow := m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.True(t, allow)
}

func TestConnMeter(t *testing.T) {
	m := NewConnMeter(time.Hour, 1, 3)

	allow := m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.False(t, allow)
}

func TestConnMeterReplace(t *testing.T) {
	remote := "127.0.0.2:9651"
	differentPort := "127.0.0.1:9650"
	m := NewConnMeter(time.Hour, 1, 1)

	allow := m.Allow(localhost)
	assert.True(t, allow)

	allow = m.Allow(differentPort)
	assert.True(t, allow)

	allow = m.Allow(remote)
	assert.True(t, allow)

	allow = m.Allow(localhost)
	assert.True(t, allow)
}
