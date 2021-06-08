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
	m := NewConnMeter(0, 1)

	count := m.Tick(localhost)
	assert.Equal(t, 0, count)

	count = m.Tick(localhost)
	assert.Equal(t, 0, count)
}

func TestConnMeter(t *testing.T) {
	m := NewConnMeter(time.Hour, 1)

	count := m.Tick(localhost)
	assert.Equal(t, 1, count)

	count = m.Tick(localhost)
	assert.Equal(t, 2, count)
}

func TestConnMeterReplace(t *testing.T) {
	remote := "127.0.0.2:9651"
	differentPort := "127.0.0.1:9650"
	m := NewConnMeter(time.Hour, 1)

	count := m.Tick(localhost)
	assert.Equal(t, 1, count)

	count = m.Tick(differentPort)
	assert.Equal(t, 1, count)

	count = m.Tick(remote)
	assert.Equal(t, 1, count)

	count = m.Tick(localhost)
	assert.Equal(t, 1, count)
}
