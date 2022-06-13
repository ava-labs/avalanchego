// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

var _ Resolver = &mockResolver{}

type mockResolver struct {
	onResolve func() (net.IP, error)
}

func (r *mockResolver) Resolve() (net.IP, error) {
	return r.onResolve()
}

func TestNewUpdater(t *testing.T) {
	assert := assert.New(t)
	originalIP := net.IPv4zero
	originalPort := 9651
	dynamicIP := ips.NewDynamicIPPort(originalIP, uint16(originalPort))
	newIP := net.IPv4(1, 2, 3, 4)
	resolver := &mockResolver{
		onResolve: func() (net.IP, error) {
			return newIP, nil
		},
	}
	updateFreq := time.Millisecond
	updaterIntf := NewUpdater(
		dynamicIP,
		resolver,
		updateFreq,
	)

	// Assert NewUpdater returns expected type
	assert.IsType(&updater{}, updaterIntf)

	updater := updaterIntf.(*updater)

	// Assert fields set
	assert.Equal(dynamicIP, updater.dynamicIP)
	assert.Equal(resolver, updater.resolver)
	assert.NotNil(updater.stopChan)
	assert.NotNil(updater.doneChan)
	assert.Equal(updateFreq, updater.updateFreq)

	// Start updating the IP address
	go updaterIntf.Dispatch(logging.NoLog{})

	// Assert that the IP is updated within 5s.
	expectedIP := ips.IPPort{
		IP:   newIP,
		Port: uint16(originalPort),
	}
	assert.Eventually(
		func() bool { return expectedIP.Equal(dynamicIP.IPPort()) },
		5*time.Second,
		updateFreq,
	)

	// Make sure stopChan and doneChan are closed when stop is called
	updaterIntf.Stop()

	stopTimeout := 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	select {
	case _, open := <-updater.stopChan:
		assert.False(open)
	case <-ctx.Done():
		assert.FailNow("timeout waiting for stopChan to close")
	}
	select {
	case _, open := <-updater.doneChan:
		assert.False(open)
	case <-ctx.Done():
		assert.FailNow("timeout waiting for doneChan to close")
	}
}
