// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"

	commonenginetest "github.com/ava-labs/avalanchego/snow/engine/enginetest"
)

// defaultMaxMessages makes the deliver loop fail fast instead of looping indefinitely if a handler is misbehaving.
const defaultMaxMessages = 50_000_000

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
	require.NotContains(n.t, n.handlers, nodeID, "node %s already registered", nodeID)
	n.handlers[nodeID] = handler
}

// DeliverMessages sends each message in the queue to its recipient. A handler
// can enqueue more messages while DeliverMessages runs, and DeliverMessages also
// sends those.
//
// When the queue is empty, DeliverMessages reports a failure for each request
// that no handler registered a response. It then dispatches those failure messages.
//
// DeliverMessages returns when the queue is empty and no request waits for an
// answer.
func (n *Network) DeliverMessages() error {
	delivered := 0
	for {
		for len(n.queue) > 0 {
			next := n.queue[0]
			n.queue = n.queue[1:]

			delivered++
			require.LessOrEqual(n.t, delivered, defaultMaxMessages,
				"network delivered more than %d messages; handlers are likely looping", defaultMaxMessages)

			if err := next(); err != nil {
				return err
			}
		}

		if len(n.awaiting) == 0 {
			return nil
		}

		// The queue is empty. No handler will answer the requests that remain.
		timedOut := n.awaiting
		n.awaiting = nil
		for _, req := range timedOut {
			n.dispatchLackOfResponse(req)
		}
	}
}

// HasQueuedMessagesToDispatch reports if the network has a message in the queue,
// or a request that waits for an answer.
func (n *Network) HasQueuedMessagesToDispatch() bool {
	return len(n.queue) > 0 || len(n.awaiting) > 0
}

// Outstanding reports the number of requests that wait for an answer.
func (n *Network) Outstanding() int {
	return len(n.awaiting)
}

func (n *Network) push(f func() error) {
	n.queue = append(n.queue, f)
}

// locateHandler finds the recipient when the network delivers the message. The
// order in which you register the nodes is therefore not important.
func (n *Network) locateHandler(nodeID ids.NodeID) common.Handler {
	handler, ok := n.handlers[nodeID]
	require.True(n.t, ok, "message sent to node %s, which was never registered", nodeID)
	return handler
}

// registerExpectingResponse records that requester waits for a response from responder. The kind
// value gives the type of the request.
func (n *Network) registerExpectingResponse(requester, responder ids.NodeID, requestID uint32, kind requestKind) {
	n.awaiting = append(n.awaiting, pendingRequest{
		requester: requester,
		responder: responder,
		requestID: requestID,
		kind:      kind,
	})
}

// registerResponse removes the request that matches a response. A responder sends its
// response with the request ID that the requester made.
func (n *Network) registerResponse(responder, requester ids.NodeID, requestID uint32, kind requestKind) {
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
		case fetchBlock:
			return handler.GetFailed(ctx, req.responder, req.requestID)
		case fetchAncestors:
			return handler.GetAncestorsFailed(ctx, req.responder, req.requestID)
		case queryPreference:
			return handler.QueryFailed(ctx, req.responder, req.requestID)
		default:
			return fmt.Errorf("unhandled request kind %d", req.kind)
		}
	})
}

type requestKind int

const (
	fetchBlock requestKind = iota
	fetchAncestors
	queryPreference
)

type pendingRequest struct {
	requester ids.NodeID
	responder ids.NodeID
	requestID uint32
	kind      requestKind
}

// Sender sends the messages from one node into the network. The embedded
// [commonenginetest.Sender] fails the test if a message kind has no route.
type Sender struct {
	commonenginetest.Sender
	network *Network
	self    ids.NodeID
}

func (s *Sender) SendGet(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
	s.network.registerExpectingResponse(s.self, nodeID, requestID, fetchBlock)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Get(context.Background(), s.self, requestID, blkID)
	})
}

func (s *Sender) SendPut(_ context.Context, nodeID ids.NodeID, requestID uint32, blk []byte) {
	s.network.registerResponse(s.self, nodeID, requestID, fetchBlock)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Put(context.Background(), s.self, requestID, blk)
	})
}

func (s *Sender) SendGetAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) {
	s.network.registerExpectingResponse(s.self, nodeID, requestID, fetchAncestors)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).GetAncestors(context.Background(), s.self, requestID, blkID)
	})
}

func (s *Sender) SendAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, blks [][]byte) {
	s.network.registerResponse(s.self, nodeID, requestID, fetchAncestors)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Ancestors(context.Background(), s.self, requestID, blks)
	})
}

func (s *Sender) SendPullQuery(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blkID ids.ID, requestedHeight uint64) {
	for _, nodeID := range sortedNodeIDs(nodeIDs) {
		s.network.registerExpectingResponse(s.self, nodeID, requestID, queryPreference)
		s.network.push(func() error {
			return s.network.locateHandler(nodeID).PullQuery(context.Background(), s.self, requestID, blkID, requestedHeight)
		})
	}
}

func (s *Sender) SendPushQuery(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, blk []byte, requestedHeight uint64) {
	for _, nodeID := range sortedNodeIDs(nodeIDs) {
		s.network.registerExpectingResponse(s.self, nodeID, requestID, queryPreference)
		s.network.push(func() error {
			return s.network.locateHandler(nodeID).PushQuery(context.Background(), s.self, requestID, blk, requestedHeight)
		})
	}
}

func (s *Sender) SendChits(_ context.Context, nodeID ids.NodeID, requestID uint32, preferredID, preferredIDAtHeight, acceptedID ids.ID, acceptedHeight uint64) {
	s.network.registerResponse(s.self, nodeID, requestID, queryPreference)
	s.network.push(func() error {
		return s.network.locateHandler(nodeID).Chits(context.Background(), s.self, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
	})
}

// sortedNodeIDs sorts the node IDs.
func sortedNodeIDs(nodeIDs set.Set[ids.NodeID]) []ids.NodeID {
	sorted := nodeIDs.List()
	slices.SortFunc(sorted, ids.NodeID.Compare)
	return sorted
}
