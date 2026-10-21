// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/message"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"

	commonenginetest "github.com/ava-labs/avalanchego/snow/engine/enginetest"
)

// maxMessages makes the deliver loop fail fast instead of looping indefinitely if a handler is misbehaving.
const maxMessages = 50_000_000

// Network is a fake network to connect engines in a test.
// The network adds each message to a queue and the messages are dispatched via DeliverMessages.
// The dispatching of messages is not done directly by invoking the remote node in order to avoid
// a stack overflow when the remote node sends a message back to the Sender and vice versa.
// Similarly, the network doesn't run each engine in a separate goroutine to avoid non-deterministic behavior in tests.
//
// To "connect" a node to the network, just register the engine to the network:
//
//	network.Register(nodeID, engine)
type Network struct {
	t        *testing.T
	handlers map[ids.NodeID]common.Handler
	queue    []func() error
	// awaiting holds each request that no handler registered a response. It keeps them in the
	// order that the senders sent them. The network then reports timeouts in the
	// same order every time.
	awaiting []pendingRequest
}

// NewNetwork returns an empty [Network].
func NewNetwork(t *testing.T) *Network {
	return &Network{
		t:        t,
		handlers: make(map[ids.NodeID]common.Handler),
	}
}

// CreateSender returns a [common.Sender] that sends messages on behalf of [nodeID].
func (n *Network) CreateSender(nodeID ids.NodeID) *Sender {
	stub := commonenginetest.Sender{T: n.t}
	stub.Default(true)
	return &Sender{
		Sender:  stub,
		network: n,
		self:    nodeID,
	}
}

// Register sets the handler that receives the messages for nodeID. Call Register
// before the network delivers a message to nodeID.
// The [handler] is often the snowman engine of the node.
func (n *Network) Register(nodeID ids.NodeID, handler common.Handler) {
	n.t.Helper()
	require.NotContains(n.t, n.handlers, nodeID, "node %s already registered", nodeID)
	n.handlers[nodeID] = handler
}

// DeliverMessages sends each message in the queue to its recipient. A handler
// can enqueue more messages while DeliverMessages runs, and DeliverMessages also
// sends those.
//
// When the queue is empty, DeliverMessages fails due to each request
// that no handler registered a response.
//
// DeliverMessages returns when the queue is empty and no request waits for an
// answer.
func (n *Network) DeliverMessages() {
	n.t.Helper()
	defer func() {
		// Make sure that the network has no more messages in the queue, or requests that waits for an answer.
		require.False(n.t, len(n.queue) > 0 || len(n.awaiting) > 0)
	}()
	delivered := 0
	for {
		for len(n.queue) > 0 {
			next := n.queue[0]
			n.queue = n.queue[1:]

			delivered++
			require.LessOrEqual(n.t, delivered, maxMessages,
				"network delivered more than %d messages; handlers are likely looping", maxMessages)

			if err := next(); err != nil {
				require.NoError(n.t, err)
			}
		}

		if len(n.awaiting) == 0 {
			return
		}

		// The queue is empty. No handler will answer the requests that remain.
		timedOut := n.awaiting
		n.awaiting = nil
		for _, req := range timedOut {
			n.dispatchLackOfResponse(req)
		}
	}
}

func (n *Network) push(f func() error) {
	n.queue = append(n.queue, f)
}

// locateHandler finds the recipient when the network delivers the message. The
// order in which you register the nodes is therefore not important.
func (n *Network) locateHandler(nodeID ids.NodeID) common.Handler {
	n.t.Helper()
	handler, ok := n.handlers[nodeID]
	require.True(n.t, ok, "message sent to node %s, which was never registered", nodeID)
	return handler
}

// registerExpectingResponse records that requester waits for a response from responder. The kind
// value gives the type of the request.
func (n *Network) registerExpectingResponse(requester, responder ids.NodeID, requestID uint32, kind message.Op) {
	n.awaiting = append(n.awaiting, pendingRequest{
		requester: requester,
		responder: responder,
		requestID: requestID,
		kind:      kind,
	})
}

// registerResponse removes the request that matches a response. A responder sends its
// response with the request ID that the requester made.
func (n *Network) registerResponse(responder, requester ids.NodeID, requestID uint32, kind message.Op) {
	n.awaiting = slices.DeleteFunc(n.awaiting, func(req pendingRequest) bool {
		return req.requester == requester &&
			req.responder == responder &&
			req.requestID == requestID &&
			req.kind == kind
	})
}

// dispatchLackOfResponse reports a request that no handler registered a response for.
func (n *Network) dispatchLackOfResponse(req pendingRequest) {
	n.push(func() error {
		handler := n.locateHandler(req.requester)
		ctx := context.Background()
		switch req.kind {
		case message.PutOp:
			return handler.GetFailed(ctx, req.responder, req.requestID)
		case message.AncestorsOp:
			return handler.GetAncestorsFailed(ctx, req.responder, req.requestID)
		case message.ChitsOp:
			return handler.QueryFailed(ctx, req.responder, req.requestID)
		default:
			return fmt.Errorf("unhandled request kind %d", req.kind)
		}
	})
}

type pendingRequest struct {
	requester ids.NodeID
	responder ids.NodeID
	requestID uint32
	kind      message.Op
}

// Sender sends the messages from one node into the network. The embedded
// [commonenginetest.Sender] fails the test if a message kind has no route.
type Sender struct {
	commonenginetest.Sender
	network *Network
	self    ids.NodeID
}

func (s *Sender) SendGet(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
	s.network.registerExpectingResponse(s.self, nodeID, requestID, message.PutOp)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Get(context.Background(), s.self, requestID, blkID)
	})
}

func (s *Sender) SendPut(_ context.Context, nodeID ids.NodeID, requestID uint32, blk []byte) {
	s.network.registerResponse(s.self, nodeID, requestID, message.PutOp)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Put(context.Background(), s.self, requestID, blk)
	})
}

func (s *Sender) SendGetAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
	s.network.registerExpectingResponse(s.self, nodeID, requestID, message.AncestorsOp)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).GetAncestors(context.Background(), s.self, requestID, blkID)
	})
}

func (s *Sender) SendAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, blks [][]byte) {
	s.network.registerResponse(s.self, nodeID, requestID, message.AncestorsOp)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Ancestors(context.Background(), s.self, requestID, blks)
	})
}

func (s *Sender) SendPullQuery(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
	for _, nodeID := range sortedNodeIDs(nodeIDs) {
		s.network.registerExpectingResponse(s.self, nodeID, requestID, message.ChitsOp)
		s.network.push(func() error {
			return s.network.locateHandler(nodeID).PullQuery(context.Background(), s.self, requestID, blkID, requestedHeight)
		})
	}
}

func (s *Sender) SendPushQuery(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blk []byte, requestedHeight uint64) {
	for _, nodeID := range sortedNodeIDs(nodeIDs) {
		s.network.registerExpectingResponse(s.self, nodeID, requestID, message.ChitsOp)
		s.network.push(func() error {
			return s.network.locateHandler(nodeID).PushQuery(context.Background(), s.self, requestID, blk, requestedHeight)
		})
	}
}

func (s *Sender) SendChits(_ context.Context, nodeID ids.NodeID, requestID uint32, _, preferredIDAtHeight, acceptedID ids.ID, acceptedHeight uint64) {
	s.network.registerResponse(s.self, nodeID, requestID, message.ChitsOp)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Chits(context.Background(), s.self, requestID, preferredIDAtHeight, acceptedID, acceptedHeight)
	})
}

func sortedNodeIDs(nodeIDs set.Set[ids.NodeID]) []ids.NodeID {
	sorted := nodeIDs.List()
	slices.SortFunc(sorted, ids.NodeID.Compare)
	return sorted
}
