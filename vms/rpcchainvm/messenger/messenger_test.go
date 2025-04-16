// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/utils/logging"

	messengerpb "github.com/ava-labs/avalanchego/proto/pb/messenger"
)

func TestMessenger(t *testing.T) {
	events := make(chan messengerpb.Message, 2)
	events <- messengerpb.Message_MESSAGE_BUILD_BLOCK
	events <- messengerpb.Message_MESSAGE_STATE_SYNC_FINISHED

	server := NewServer(&logging.NoLog{})
	getMsg := func(context.Context) messengerpb.Message {
		return <-events
	}
	client := NewClient(&fakeClient{server: server}, &logging.NoLog{}, getMsg)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.Run()
	}()
	msg, err := server.SubscribeToEvents(context.Background())
	require.NoError(t, err)
	require.Equal(t, messengerpb.Message_MESSAGE_BUILD_BLOCK, messengerpb.Message(msg))

	msg, err = server.SubscribeToEvents(context.Background())
	require.NoError(t, err)
	require.Equal(t, messengerpb.Message_MESSAGE_STATE_SYNC_FINISHED, messengerpb.Message(msg))
	client.Stop()
	wg.Wait()
}

func TestMessengerAbort(t *testing.T) {
	events := make(chan messengerpb.Message, 1)

	server := NewServer(&logging.NoLog{})
	getMsg := func(context.Context) messengerpb.Message {
		return <-events
	}
	client := NewClient(&fakeClient{server: server}, &logging.NoLog{}, getMsg)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.Run()
	}()
	context, cancel := context.WithCancel(context.Background())
	cancel()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	go func() {
		select {
		case <-timer.C:
		case events <- messengerpb.Message_MESSAGE_BUILD_BLOCK:
		}
	}()
	msg, err := server.SubscribeToEvents(context)
	require.EqualError(t, err, "stream is nil, subscription attempted timed out or was cancelled")
	require.Equal(t, messengerpb.Message_MESSAGE_UNSPECIFIED, messengerpb.Message(msg))
	client.Stop()
	wg.Wait()
}

type fakeClient struct {
	server *Server
}

func (f fakeClient) Notify(ctx context.Context, _ ...grpc.CallOption) (messengerpb.Messenger_NotifyClient, error) {
	cs := &clientStream{
		ctx:        ctx,
		toServer:   make(chan interface{}),
		fromServer: make(chan interface{}),
	}
	go f.server.Notify(&serverStream{
		ctx:        ctx,
		fromClient: cs.toServer,
		toClient:   cs.fromServer,
	})
	return cs, nil
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
	toClient   chan interface{}
	fromClient chan interface{}
}

func (ss *serverStream) Context() context.Context {
	return ss.ctx
}

func (ss *serverStream) Send(req *messengerpb.EventRequest) error {
	ss.toClient <- req
	return nil
}

func (ss *serverStream) Recv() (*messengerpb.Event, error) {
	event := <-ss.fromClient
	return event.(*messengerpb.Event), nil
}

type clientStream struct {
	ctx context.Context
	grpc.ClientStream
	toServer   chan interface{}
	fromServer chan interface{}
}

func (cs *clientStream) Context() context.Context {
	return cs.ctx
}

func (cs *clientStream) Send(event *messengerpb.Event) error {
	cs.toServer <- event
	return nil
}

func (cs *clientStream) Recv() (*messengerpb.EventRequest, error) {
	select {
	case req := <-cs.fromServer:
		return req.(*messengerpb.EventRequest), nil
	case <-cs.ctx.Done():
		return nil, cs.ctx.Err()
	}
}
