// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

var _ RequestHandler = NoopRequestHandler{}

// RequestHandler interface handles incoming requests from peers
// Must have methods in format of handleType(context.Context, ids.NodeID, uint32, request Type) error
// so that the Request object of relevant Type can invoke its respective handle method
// on this struct.
// Also see GossipHandler for implementation style.
type RequestHandler interface {
	HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest LeafsRequest) ([]byte, error)
	HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request BlockRequest) ([]byte, error)
	HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest CodeRequest) ([]byte, error)
}

// ResponseHandler handles response for a sent request
// Only one of OnResponse or OnFailure is called for a given requestID, not both
type ResponseHandler interface {
	// OnResponse is invoked when the peer responded to a request
	OnResponse(response []byte) error
	// OnFailure is invoked when there was a failure in processing a request
	OnFailure() error
}

type NoopRequestHandler struct{}

func (NoopRequestHandler) HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest LeafsRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request BlockRequest) ([]byte, error) {
	return nil, nil
}

func (NoopRequestHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest CodeRequest) ([]byte, error) {
	return nil, nil
}
