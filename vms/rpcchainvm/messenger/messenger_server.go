// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"fmt"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
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
	logger logging.Logger

	lock   sync.Mutex
	signal sync.Cond
}

// NewServer returns a messenger connected to a remote channel
func NewServer(logger logging.Logger) *Server {
	s := &Server{
		logger: logger,
		events: make(chan common.Message),
	}

	s.signal = sync.Cond{L: &s.lock}

	return s
}

func (s *Server) SubscribeToEvents(ctx context.Context, pChainHeight uint64) (common.Message, uint64, error) {
	// Wait for the stream to be connected
	stream := s.acquireStream(ctx)
	// If stream is nil, it means the context was done before the stream was connected
	if stream == nil {
		return 0, 0, fmt.Errorf("stream is nil, subscription attempted timed out or was cancelled")
	}

	if err := stream.Send(startRequest(pChainHeight)); err != nil {
		s.logger.Error("failed to send subscription request", zap.Error(err))
		return 0, 0, fmt.Errorf("failed to send subscription request: %w", err)
	}

	select {
	case <-ctx.Done():
		if err := stream.Send(stopRequest()); err != nil {
			s.logger.Error("failed to send stop request", zap.Error(err))
			return 0, 0, fmt.Errorf("failed to send stop request: %w", err)
		}
		return 0, 0, nil
	case event := <-s.events:
		s.logger.Debug("received event", zap.Any("event", event))
		// We return the given pChainHeight as the RPC VM cannot have the P-chain height, only the embedded VM
		return event, pChainHeight, nil
	}
}

func startRequest(pChainHeight uint64) *messengerpb.EventRequest {
	return &messengerpb.EventRequest{Event: &messengerpb.EventRequest_Start{Start: true}, PChainHeight: pChainHeight}
}

func stopRequest() *messengerpb.EventRequest {
	return &messengerpb.EventRequest{Event: &messengerpb.EventRequest_Stop{Stop: true}}
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

		s.logger.Debug("received event from RPC VM", zap.Any("event", event))

		select {
		case s.events <- common.Message(event.GetMessage()):
		case <-stream.Context().Done():
			return nil
		}
	}
}
