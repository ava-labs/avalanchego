// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"sync"

	messengerpb "github.com/ava-labs/avalanchego/proto/pb/messenger"
)

var (
	_ messengerpb.MessengerServer = (*Server)(nil)
)

// Server is a messenger that is managed over RPC.
type Server struct {
	messengerpb.UnsafeMessengerServer
	stream messengerpb.Messenger_NotifyServer
	events chan common.Message

	lock   sync.Mutex
	signal sync.Cond
}

// NewServer returns a messenger connected to a remote channel
func NewServer() *Server {
	s := &Server{
		events: make(chan common.Message),
	}

	s.signal = sync.Cond{L: &s.lock}

	return s
}

func (s *Server) SubscribeToEvents(ctx context.Context) common.Message {
	// Wait for the stream to be connected
	stream := s.acquireStream(ctx)
	// If stream is nil, it means the context was done before the stream was connected
	if stream == nil {
		return 0
	}

	if err := stream.Send(&messengerpb.EventRequest{Event: &messengerpb.EventRequest_Start{Start: true}}); err != nil {
		return 0
	}

	select {
	case <-ctx.Done():
		stream.Send(&messengerpb.EventRequest{Event: &messengerpb.EventRequest_Stop{Stop: true}}) // TODO: handle send error
		return 0
	case event := <-s.events:
		return event
	}
}

func (s *Server) acquireStream(ctx context.Context) messengerpb.Messenger_NotifyServer {
	var stream messengerpb.Messenger_NotifyServer
	s.lock.Lock()
	defer s.lock.Unlock()

	context, cancel := context.WithCancel(ctx)
	go func() {
		<-context.Done()
		s.signal.Signal()
	}()

	defer cancel()

	for {
		select {
		case <-context.Done():
			return nil
		default:
		}

		stream = s.stream
		if stream == nil {
			s.signal.Wait()
		} else {
			return stream
		}
	}
}

func (s *Server) Notify(stream messengerpb.Messenger_NotifyServer) error {
	s.lock.Lock()
	s.stream = stream
	s.signal.Signal()
	s.lock.Unlock()

	for {
		event, err := stream.Recv()
		if err != nil {
			return err
		}

		select {
		case s.events <- common.Message(event.GetMessage()):
		case <-stream.Context().Done():
			return nil
		}
	}
}
