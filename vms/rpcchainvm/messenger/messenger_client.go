// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
	"sync"
	"time"

	messengerpb "github.com/ava-labs/avalanchego/proto/pb/messenger"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client          messengerpb.MessengerClient
	log             logging.Logger
	getMessage      func(ctx context.Context) messengerpb.Message
	cancel          context.CancelFunc
	shutdown        context.CancelFunc
	shutdownContext context.Context
}

// NewClient returns a client that is connected to a remote channel
func NewClient(client messengerpb.MessengerClient, log logging.Logger, getMessage func(ctx context.Context) messengerpb.Message) *Client {
	shutdownCtx, shutdownCtxCancel := context.WithCancel(context.Background())
	return &Client{
		shutdownContext: shutdownCtx,
		shutdown:        shutdownCtxCancel,
		log:             log,
		client:          client,
		getMessage:      getMessage,
	}
}

func (c *Client) Run() {
	var stream messengerpb.Messenger_NotifyClient
	var err error

	for {
		select {
		case <-c.shutdownContext.Done():
			return
		default:

		}
		if stream == nil {
			stream, err = c.client.Notify(c.shutdownContext)
			if err != nil {
				c.log.Error("Error creating stream", zap.Error(err))
				time.Sleep(time.Second)
				continue
			}
		}

		req, err := stream.Recv()
		if err != nil {
			c.log.Error("Error receiving message", zap.Error(err))
			stream = nil
			time.Sleep(time.Second)
			continue
		}

		c.handleRequest(req, stream)
	}
}

func (c *Client) Stop() {
	select {
	case <-c.shutdownContext.Done():
		return
	default:
		c.shutdown()
	}
}

func (c *Client) handleRequest(req *messengerpb.EventRequest, stream messengerpb.Messenger_NotifyClient) {
	if req.GetStart() {
		c.dispatchStart(stream)
		return
	}

	if req.GetStop() {
		c.cancel()
	}
}

func (c *Client) dispatchStart(stream messengerpb.Messenger_NotifyClient) {
	// First, cancel the previous execution
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	var wg sync.WaitGroup
	wg.Add(1)

	ctx, cancel := context.WithCancel(stream.Context())
	c.cancel = func() {
		cancel()
		wg.Wait()
	}

	go func() {
		defer wg.Done()
		c.sendMessageBack(ctx, stream)
	}()
}

func (c *Client) sendMessageBack(ctx context.Context, stream messengerpb.Messenger_NotifyClient) {
	msg := c.getMessage(ctx)

	// If the context is prematurely cancelled, don't send anything back.
	select {
	case <-ctx.Done():
		return
	default:
	}

	if err := stream.Send(&messengerpb.Event{Message: msg}); err != nil {
		c.log.Error("Error sending message", zap.Error(err))
	}
}
