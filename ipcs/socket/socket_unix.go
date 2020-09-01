// +build !windows,!plan9,!js

// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"net"
	"os"
	"syscall"
	"time"
)

// Listen starts listening on the socket for new connection
func (s *Socket) Listen() error {
	addr, err := net.ResolveUnixAddr("unix", s.addr)
	if err != nil {
		return err
	}

	// Try to listen on the socket. If that fails we check to see if it's a stale
	// socket and remove it if it is. Then we try to listen one more time.
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		if err = removeIfStaleUnixSocket(s.addr); err != nil {
			return err
		}
		if l, err = net.ListenUnix("unix", addr); err != nil {
			return err
		}
	}

	// Start a loop that accepts new connections to told to quit
	go func() {
		for {
			select {
			case <-s.quitCh:
				close(s.doneCh)
				return
			default:
				conn, err := l.AcceptUnix()
				if err != nil {
					s.log.Error("socket accept error: %s", err.Error())
				}
				s.connLock.Lock()
				s.conns = append(s.conns, conn)
				s.connLock.Unlock()
			}
		}
	}()

	return nil
}

// Dial creates a new *Client connected to the given address
func Dial(addr string) (*Client, error) {
	unixAddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		return nil, err
	}

	c, err := net.DialUnix("unix", nil, unixAddr)
	if err != nil {
		if isTimeoutError(err) {
			return nil, ErrTimeout
		}
		return nil, err
	}

	return &Client{c}, nil
}

// removeIfStaleUnixSocket takes in a path and removes it iff it is a socket
// that is refusing connections
func removeIfStaleUnixSocket(socketPath string) error {
	// Ensure it's a socket; if not return without an error
	if st, err := os.Stat(socketPath); err != nil || st.Mode()&os.ModeType != os.ModeSocket {
		return nil
	}

	// Try to connect
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)

	switch {
	// The connection was refused so this socket is stale; remove it
	case isSyscallError(err, syscall.ECONNREFUSED):
		return os.Remove(socketPath)

	// The socket is alive so close this connection and leave the socket alone
	case err == nil:
		return conn.Close()
	}
	return nil
}
