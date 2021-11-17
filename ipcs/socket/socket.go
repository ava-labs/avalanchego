// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// ErrMessageTooLarge is returned when reading a message that is larger than
// our max size
var ErrMessageTooLarge = errors.New("message too large")

// Socket manages sending messages over a socket to many subscribed clients
type Socket struct {
	log      logging.Logger
	addr     string
	accept   acceptFn
	connLock *sync.RWMutex
	conns    map[net.Conn]struct{}
	quitCh   chan struct{}
	doneCh   chan struct{}
	listener net.Listener // the current listener
}

// NewSocket creates a new socket object for the given address. It does not open
// the socket until Listen is called.
func NewSocket(addr string, log logging.Logger) *Socket {
	return &Socket{
		log:      log,
		addr:     addr,
		accept:   accept,
		connLock: &sync.RWMutex{},
		conns:    map[net.Conn]struct{}{},
		quitCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Listen starts listening on the socket for new connection
func (s *Socket) Listen() error {
	l, err := listen(s.addr)
	if err != nil {
		return err
	}
	s.listener = l

	// Start a loop that accepts new connections until told to quit
	go func() {
		for {
			select {
			case <-s.quitCh:
				close(s.doneCh)
				return
			default:
				s.accept(s, l)
			}
		}
	}()

	return nil
}

// Send writes the given message to all connection clients
func (s *Socket) Send(msg []byte) {
	var conns []net.Conn

	// Get a copy of connections
	s.connLock.RLock()
	if len(s.conns) > 0 {
		conns = make([]net.Conn, len(s.conns))
		i := 0
		for conn := range s.conns {
			conns[i] = conn
			i++
		}
	}
	s.connLock.RUnlock()

	// Write to each connection
	if len(conns) == 0 {
		return
	}

	// Prefix the message with an 8 byte length
	lenBytes := [8]byte{}
	binary.BigEndian.PutUint64(lenBytes[:], uint64(len(msg)))
	for _, conn := range conns {
		for _, byteSlice := range [][]byte{lenBytes[:], msg} {
			if _, err := conn.Write(byteSlice); err != nil {
				s.removeConn(conn)
				s.log.Debug("failed to write message to %s: %s", conn.RemoteAddr(), err)
			}
		}
	}
}

// Close closes the socket by cutting off new connections, closing all
// existing ones, and then zero'ing out the connection pool
func (s *Socket) Close() error {
	// Signal to the event loop to stop and wait for it to signal back
	close(s.quitCh)

	listener := s.listener
	s.listener = nil

	// close the listener to break the loop
	err := listener.Close()

	<-s.doneCh

	// Zero out the connection pool but save a reference so we can close them all
	s.connLock.Lock()
	conns := s.conns
	s.conns = nil
	s.connLock.Unlock()

	// Close all connections that were open at the time of shutdown
	errs := wrappers.Errs{Err: err}
	for conn := range conns {
		if conn != nil {
			errs.Add(conn.Close())
		}
	}
	return errs.Err
}

func (s *Socket) Running() bool {
	return s.listener != nil
}

func (s *Socket) removeConn(c net.Conn) {
	s.connLock.Lock()
	delete(s.conns, c)
	s.connLock.Unlock()
}

// Client is a read-only connection to a socket
type Client struct {
	net.Conn
	maxMessageSize int64
}

// Recv waits for a message from the socket. It's guaranteed to either return a
// complete message or an error
func (c *Client) Recv() ([]byte, error) {
	// Read length
	var sz uint64
	if err := binary.Read(c.Conn, binary.BigEndian, &sz); err != nil {
		if isTimeoutError(err) {
			return nil, errReadTimeout{c.Conn.RemoteAddr()}
		}
		return nil, err
	}

	if sz > uint64(atomic.LoadInt64(&c.maxMessageSize)) {
		return nil, ErrMessageTooLarge
	}

	// Create buffer for entire message and read it all in
	msg := make([]byte, sz)
	if _, err := io.ReadFull(c.Conn, msg); err != nil {
		if isTimeoutError(err) {
			return nil, errReadTimeout{c.Conn.RemoteAddr()}
		}
		return nil, err
	}

	return msg, nil
}

// SetMaxMessageSize sets the maximum size to allow for messages
func (c *Client) SetMaxMessageSize(s int64) {
	atomic.StoreInt64(&c.maxMessageSize, s)
}

// Close closes the underlying socket connection
func (c *Client) Close() error {
	return c.Conn.Close()
}

// errReadTimeout is returned a socket read times out
type errReadTimeout struct {
	addr net.Addr
}

// Error implements the error interface
func (e errReadTimeout) Error() string {
	return fmt.Sprintf("read from %s timed out", e.addr)
}

// acceptFn takes accepts connections from a Listener and gives them to a Socket
type acceptFn func(*Socket, net.Listener)

// accept is the default acceptFn for sockets. It accepts the next connection
// from the given listener and adds it to the Socket's connection list
func accept(s *Socket, l net.Listener) {
	conn, err := l.Accept()
	if err != nil {
		if !s.Running() {
			return
		}
		s.log.Error("socket accept error: %s", err.Error())
	}
	if conn, ok := conn.(*net.TCPConn); ok {
		if err := conn.SetLinger(0); err != nil {
			s.log.Warn("failed to set no linger due to: %s", err)
		}
		if err := conn.SetNoDelay(true); err != nil {
			s.log.Warn("failed to set socket nodelay due to: %s", err)
		}
	}
	s.connLock.Lock()
	s.conns[conn] = struct{}{}
	s.connLock.Unlock()
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
