// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test that canceling a context passed into Dial results
// in giving up trying to connect
func TestDialerDialCanceledContext(t *testing.T) {
	require := require.New(t)

	listenAddrPort := netip.AddrPortFrom(netip.IPv4Unspecified(), 0)
	dialer := NewDialer("tcp", Config{}, logging.NoLog{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := dialer.Dial(ctx, listenAddrPort)
	require.ErrorIs(err, context.Canceled)
}

func TestDialerDial(t *testing.T) {
	require := require.New(t)

	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(err)

	listenedAddrPort, err := netip.ParseAddrPort(l.Addr().String())
	require.NoError(err)

	dialer := NewDialer(
		"tcp",
		Config{
			ThrottleRps:       10,
			ConnectionTimeout: 30 * time.Second,
		},
		logging.NoLog{},
	)

	eg := errgroup.Group{}
	eg.Go(func() error {
		_, err := dialer.Dial(t.Context(), listenedAddrPort)
		return err
	})

	_, err = l.Accept()
	require.NoError(err)

	require.NoError(eg.Wait())
	require.NoError(l.Close())
}
