// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Resolver = (*mockResolver)(nil)

type mockResolver struct {
	onResolve func() (net.IP, error)
}

func (r *mockResolver) Resolve() (net.IP, error) {
	return r.onResolve()
}

func TestNewUpdater(t *testing.T) {
	require := require.New(t)
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
	require.IsType(&updater{}, updaterIntf)

	updater := updaterIntf.(*updater)

	// Assert fields set
	require.Equal(dynamicIP, updater.dynamicIP)
	require.Equal(resolver, updater.resolver)
	require.NotNil(updater.stopChan)
	require.NotNil(updater.doneChan)
	require.Equal(updateFreq, updater.updateFreq)

	// Start updating the IP address
	go updaterIntf.Dispatch(logging.NoLog{})

	// Assert that the IP is updated within 5s.
	expectedIP := ips.IPPort{
		IP:   newIP,
		Port: uint16(originalPort),
	}
	require.Eventually(
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
		require.False(open)
	case <-ctx.Done():
		require.FailNow("timeout waiting for stopChan to close")
	}
	select {
	case _, open := <-updater.doneChan:
		require.False(open)
	case <-ctx.Done():
		require.FailNow("timeout waiting for doneChan to close")
	}
}
