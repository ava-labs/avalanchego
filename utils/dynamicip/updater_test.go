// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Resolver = (*mockResolver)(nil)

type mockResolver struct {
	onResolve func(context.Context) (netip.Addr, error)
}

func (r *mockResolver) Resolve(ctx context.Context) (netip.Addr, error) {
	return r.onResolve(ctx)
}

func TestNewUpdater(t *testing.T) {
	require := require.New(t)

	const (
		port            = 9651
		updateFrequency = time.Millisecond
		stopTimeout     = 5 * time.Second
	)

	var (
		originalAddr        = netip.IPv4Unspecified()
		originalAddrPort    = netip.AddrPortFrom(originalAddr, port)
		newAddr             = netip.AddrFrom4([4]byte{1, 2, 3, 4})
		expectedNewAddrPort = netip.AddrPortFrom(newAddr, port)
		dynamicIP           = utils.NewAtomic(originalAddrPort)
	)
	resolver := &mockResolver{
		onResolve: func(context.Context) (netip.Addr, error) {
			return newAddr, nil
		},
	}
	updaterIntf := NewUpdater(
		dynamicIP,
		resolver,
		updateFrequency,
	)

	// Assert NewUpdater returns expected type
	require.IsType(&updater{}, updaterIntf)
	updater := updaterIntf.(*updater)

	// Assert fields set
	require.Equal(dynamicIP, updater.dynamicIP)
	require.Equal(resolver, updater.resolver)
	require.NotNil(updater.rootCtx)
	require.NotNil(updater.rootCtxCancel)
	require.NotNil(updater.doneChan)
	require.Equal(updateFrequency, updater.updateFreq)

	// Start updating the IP address
	go updater.Dispatch(logging.NoLog{})

	// Assert that the IP is updated within 5s.
	require.Eventually(
		func() bool {
			return dynamicIP.Get() == expectedNewAddrPort
		},
		5*time.Second,
		updateFrequency,
	)

	// Make sure stopChan and doneChan are closed when stop is called
	updater.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	select {
	case <-updater.rootCtx.Done():
	case <-ctx.Done():
		require.FailNow("timeout waiting for root context cancellation")
	}
	select {
	case _, open := <-updater.doneChan:
		require.False(open)
	case <-ctx.Done():
		require.FailNow("timeout waiting for doneChan to close")
	}
}
