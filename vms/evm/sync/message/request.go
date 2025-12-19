// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

// Request represents a Network request type.
type Request interface {
	// Stringer enables requests to implement String() for logging.
	fmt.Stringer

	// Handle allows `Request` to call respective methods on handler to handle
	// this particular request type.
	Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error)
}

// RequestToBytes marshals the given request object into bytes using the provided codec.
func RequestToBytes(c codec.Manager, request Request) ([]byte, error) {
	return c.Marshal(Version, &request)
}

var _ RequestHandler = NoopRequestHandler{}

// RequestHandler handles incoming requests from peers.
// Each request type has a corresponding handler method that processes the request
// and returns a response or an error.
type RequestHandler interface {
	HandleLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest LeafsRequest) ([]byte, error)
	HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request BlockRequest) ([]byte, error)
	HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest CodeRequest) ([]byte, error)
}

// ResponseHandler handles responses for sent requests.
// Only one of [ResponseHandler.OnResponse] or [ResponseHandler.OnFailure] is called for a given requestID, not both.
type ResponseHandler interface {
	// OnResponse is invoked when the peer responded to a request.
	OnResponse(response []byte) error
	// OnFailure is invoked when there was a failure in processing a request.
	OnFailure() error
}

// NoopRequestHandler is a no-op implementation of RequestHandler that does nothing.
type NoopRequestHandler struct{}

func (NoopRequestHandler) HandleLeafsRequest(context.Context, ids.NodeID, uint32, LeafsRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleBlockRequest(context.Context, ids.NodeID, uint32, BlockRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleCodeRequest(context.Context, ids.NodeID, uint32, CodeRequest) ([]byte, error) {
	return nil, nil
}
