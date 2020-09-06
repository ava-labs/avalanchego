package socket

import (
	"net"
	"testing"
)

func TestSocketSendAndReceive(t *testing.T) {
	var (
		socketName = "/tmp/pipe-test.sock"
		msg        = append([]byte("avalanche"), make([]byte, 1000000)...)
		connCh     chan net.Conn
	)

	// Create socket and client; wait for client to connect
	socket := NewSocket(socketName, nil)
	if err := socket.Listen(); err != nil {
		t.Fatal("Failed to listen on socket:", err.Error())
	}

	socket.accept, connCh = newTestAcceptFn()

	client, err := Dial(socketName)
	if err != nil {
		t.Fatal("Failed to dial socket:", err.Error())
	}
	<-connCh

	// Start sending in the background
	go func() {
		if err := socket.Send(msg); err != nil {
			t.Fatal("Failed to send to socket:", err.Error())
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
		s.conns = append(s.conns, conn)
		s.connLock.Unlock()

		connCh <- conn
	}, connCh
}
