// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

// Test that canceling a context passed into Dial results
// in giving up trying to connect
func TestDialerCancelDial(t *testing.T) {
	done := make(chan struct{}, 1)

	var (
		l           net.Listener
		err         error
		setupWg     sync.WaitGroup
		connsMadeWg sync.WaitGroup
	)
	setupWg.Add(1)
	connsMadeWg.Add(5)

	go func() {
		// Continuously accept connections from myself
		l, err = net.Listen("tcp", "127.0.0.1:")
		if err != nil {
			t.Error(err)
		}
		setupWg.Done()
		for {
			_, err := l.Accept()
			if err != nil {
				// Distinguish between an error that occurred because
				// the test is over from actual errors
				select {
				case <-done:
					return
				default:
					t.Error(err)
					return
				}
			}
			connsMadeWg.Done()
		}
	}()
	// Wait until [l] has been populated to avoid race condition
	setupWg.Wait()

	port, _ := strconv.Atoi(strings.Split(l.Addr().String(), ":")[1])
	myIP := utils.IPDesc{
		IP:   net.ParseIP("127.0.0.1"),
		Port: uint16(port),
	}

	// Create a dialer that should allow 10 outgoing connections per second
	dialer := NewDialer("tcp", Config{ThrottleRps: 10, ConnectionTimeout: 30 * time.Second}, logging.NoLog{})
	// Make 5 outgoing connections. Should not be throttled.
	for i := 0; i < 5; i++ {
		startTime := time.Now()
		_, err := dialer.Dial(context.Background(), myIP)
		assert.NoError(t, err)
		// Connecting to myself shouldn't take more than 50 ms if outgoing
		// connections aren't throttled
		assert.WithinDuration(t, startTime, time.Now(), 50*time.Millisecond)
	}

	// Make another outgoing connection but immediately cancel the context
	// (actually we cancel it before calling Dial but same difference)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sixthDialDone := make(chan struct{}, 1)
	go func() {
		_, err := dialer.Dial(ctx, myIP)
		assert.Error(t, err)
		close(sixthDialDone)
	}()

	// First 5 connections should have succeeded but not the 6th, cancelled one
	connsMadeWg.Wait()
	// Don't exit test before we assert that the sixth Dial attempt errors
	<-sixthDialDone
	done <- struct{}{} // mark that test is done
	_ = l.Close()
}
