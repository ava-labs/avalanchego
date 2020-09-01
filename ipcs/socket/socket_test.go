package socket

import (
	"testing"
)

func TestSocketSendAndReceive(t *testing.T) {
	var (
		socketName = "/tmp/pipe-test.sock"
		msg        = append([]byte("avalanche"), make([]byte, 1000000)...)
	)

	// Create socket and client
	socket := NewSocket(socketName, nil)
	if err := socket.Listen(); err != nil {
		t.Fatal("Failed to listen on socket:", err.Error())
	}

	client, err := Dial(socketName)
	if err != nil {
		t.Fatal("Failed to dial socket:", err.Error())
	}

	// Start a goroutine that waits to receive a message and checks that it's
	// correct
	doneCh := make(chan struct{})
	readyCh := make(chan struct{})
	go func() {
		close(readyCh)
		defer close(doneCh)

		// Receive message and compare it to what was sent
		receivedMsg, err := client.Recv()
		if err != nil {
			t.Fatal("Failed to receive from socket:", err.Error())
		}
		if string(receivedMsg) != string(msg) {
			t.Fatal("Received incorrect message:", string(msg))
		}
	}()

	// Wait for the receiving goroutine to to have started, then send our message,
	// then wait for the receiving goroutine to finish
	<-readyCh
	if err := socket.Send(msg); err != nil {
		t.Fatal("Failed to send to socket:", err.Error())
	}
	<-doneCh
}
