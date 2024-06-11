// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Test that canceling a context passed into Dial results
// in giving up trying to connect
func TestDialerCancelDial(t *testing.T) {
	require := require.New(t)

	listenAddrPort := netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		0,
	)
	l, err := net.Listen("tcp", listenAddrPort.String())
	require.NoError(err)

	done := make(chan struct{})
	go func() {
		for {
			// Continuously accept connections from myself
			_, err := l.Accept()
			if err != nil {
				// Distinguish between an error that occurred because
				// the test is over from actual errors
				select {
				case <-done:
					return
				default:
					require.FailNow(err.Error())
				}
			}
		}
	}()

	listenedAddrPort, err := netip.ParseAddrPort(l.Addr().String())
	require.NoError(err)

	// Create a dialer
	dialer := NewDialer(
		"tcp",
		Config{
			ThrottleRps:       10,
			ConnectionTimeout: 30 * time.Second,
		},
		logging.NoLog{},
	)

	// Make an outgoing connection with a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dialer.Dial(ctx, listenedAddrPort)
	require.ErrorIs(err, context.Canceled)

	// Make an outgoing connection with a non-cancelled context
	conn, err := dialer.Dial(context.Background(), listenedAddrPort)
	require.NoError(err)
	_ = conn.Close()

	close(done) // stop listener goroutine
	_ = l.Close()
}
