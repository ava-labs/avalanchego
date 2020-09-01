// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

var (
	// ErrTimeout is returned when a socket operation times out
	ErrTimeout = errors.New("timeout")
)

// Socket manages sending messages over a socket to many subscribed clients
type Socket struct {
	log logging.Logger

	addr     string
	connLock *sync.RWMutex
	conns    []net.Conn

	listeningCh chan struct{}
	quitCh      chan struct{}
	doneCh      chan struct{}
}

// NewSocket creates a new socket object for the given address. It does not open
// the socket until Listen is called.
func NewSocket(addr string, log logging.Logger) *Socket {
	return &Socket{
		log: log,

		addr:     addr,
		connLock: &sync.RWMutex{},

		listeningCh: make(chan struct{}),
		quitCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

// Send writes the given message to all connection clients
func (s *Socket) Send(msg []byte) error {
	// Prefix the message with an 8 byte length
	lenBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBytes, uint64(len(msg)))
	msg = append(lenBytes, msg...)

	// Get a copy of connections
	s.connLock.RLock()
	conns := make([]net.Conn, len(s.conns))
	copy(conns, s.conns)
	s.connLock.RUnlock()

	// Write to each connection
	var err error
	for _, conn := range conns {
		if _, err = conn.Write(msg); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the socket by cutting off new connections, closing all
// existing ones, and then zero'ing out the connection pool
func (s *Socket) Close() error {
	// Signal to the event loop to stop and wait for it to signal back
	close(s.quitCh)
	<-s.doneCh

	// Zero out the connection pool but save a reference so we can close them all
	s.connLock.Lock()
	conns := s.conns
	s.conns = nil
	s.connLock.Unlock()

	// Close all connections that were open at the time of shutdown
	errs := wrappers.Errs{}
	for _, conn := range conns {
		errs.Add(conn.Close())
	}
	return errs.Err
}

// Client is a read-only connection to a socket
type Client struct {
	net.Conn
}

// Recv waits for a message from the socket. It's guaranteed to either return a
// complete message or an error
func (c *Client) Recv() ([]byte, error) {
	// Read length
	var sz int64
	if err := binary.Read(c.Conn, binary.BigEndian, &sz); err != nil {
		if isTimeoutError(err) {
			return nil, ErrTimeout
		}
		return nil, err
	}

	// Create buffer for entire message and read it all in
	msg := make([]byte, sz)
	if _, err := io.ReadFull(c.Conn, msg); err != nil {
		if isTimeoutError(err) {
			return nil, ErrTimeout
		}
		return nil, err
	}

	return msg, nil
}

// Close closes the underlying socket connection
func (c *Client) Close() error {
	return c.Conn.Close()
}

// isTimeoutError checks if an error is a timeout as per the net.Error interface
func isTimeoutError(err error) bool {
	iErr, ok := err.(net.Error)
	if !ok {
		return false
	}
	return iErr.Timeout()
}

// isSyscallError checks if an error is one of the given syscall.Errno codes
func isSyscallError(err error, codes ...syscall.Errno) bool {
	opErr, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	syscallErr, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		return false
	}
	errno, ok := syscallErr.Err.(syscall.Errno)
	if !ok {
		return false
	}
	for _, code := range codes {
		if errno == code {
			return true
		}
	}
	return false
}
