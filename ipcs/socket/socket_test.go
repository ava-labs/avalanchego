// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSocketSendAndReceive(t *testing.T) {
	require := require.New(t)

	var (
		connCh     chan net.Conn
		socketName = "/tmp/pipe-test.sock"
		msg        = append([]byte("avalanche"), make([]byte, 1000000)...)
		msgLen     = int64(len(msg))
	)

	// Create socket and client; wait for client to connect
	socket := NewSocket(socketName, nil)
	socket.accept, connCh = newTestAcceptFn(t)
	require.NoError(socket.Listen())

	client, err := Dial(socketName)
	require.NoError(err)
	<-connCh

	// Start sending in the background
	go func() {
		for {
			socket.Send(msg)
		}
	}()

	// Receive message and compare it to what was sent
	receivedMsg, err := client.Recv()
	require.NoError(err)
	require.Equal(msg, receivedMsg)

	// Test max message size
	client.SetMaxMessageSize(msgLen)
	_, err = client.Recv()
	require.NoError(err)

	client.SetMaxMessageSize(msgLen - 1)
	_, err = client.Recv()
	require.ErrorIs(err, ErrMessageTooLarge)
}

// newTestAcceptFn creates a new acceptFn and a channel that receives all new
// connections
func newTestAcceptFn(t *testing.T) (acceptFn, chan net.Conn) {
	connCh := make(chan net.Conn)

	return func(s *Socket, l net.Listener) {
		conn, err := l.Accept()
		require.NoError(t, err)

		s.connLock.Lock()
		s.conns[conn] = struct{}{}
		s.connLock.Unlock()

		connCh <- conn
	}, connCh
}
