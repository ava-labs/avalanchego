// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"net"
	"testing"
)

func TestSocketSendAndReceive(t *testing.T) {
	var (
		connCh     chan net.Conn
		socketName = "/tmp/pipe-test.sock"
		msg        = append([]byte("avalanche"), make([]byte, 1000000)...)
		msgLen     = int64(len(msg))
	)

	// Create socket and client; wait for client to connect
	socket := NewSocket(socketName, nil)
	socket.accept, connCh = newTestAcceptFn()
	if err := socket.Listen(); err != nil {
		t.Fatal("Failed to listen on socket:", err.Error())
	}

	client, err := Dial(socketName)
	if err != nil {
		t.Fatal("Failed to dial socket:", err.Error())
	}
	<-connCh

	// Start sending in the background
	go func() {
		for {
			socket.Send(msg)
		}
	}()

	// Receive message and compare it to what was sent
	receivedMsg, err := client.Recv()
	if err != nil {
		t.Fatal("Failed to receive from socket:", err.Error())
	}
	if string(receivedMsg) != string(msg) {
		t.Fatal("Received incorrect message:", string(msg))
	}

	// Test max message size
	client.SetMaxMessageSize(msgLen)
	if _, err = client.Recv(); err != nil {
		t.Fatal("Failed to receive from socket:", err.Error())
	}
	client.SetMaxMessageSize(msgLen - 1)
	if _, err = client.Recv(); err != ErrMessageTooLarge {
		t.Fatal("Should have received message too large error, got:", err)
	}
}

// newTestAcceptFn creates a new acceptFn and a channel that receives all new
// connections
func newTestAcceptFn() (acceptFn, chan net.Conn) {
	connCh := make(chan net.Conn)

	return func(s *Socket, l net.Listener) {
		conn, err := l.Accept()
		if err != nil {
			s.log.Error("socket accept error: %s", err.Error())
		}

		s.connLock.Lock()
		s.conns[conn] = struct{}{}
		s.connLock.Unlock()

		connCh <- conn
	}, connCh
}
