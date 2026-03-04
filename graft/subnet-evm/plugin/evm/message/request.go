// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
)

// Request represents a Network request type
type Request interface {
	// Requests should implement String() for logging.
	fmt.Stringer

	// Handle allows `Request` to call respective methods on handler to handle
	// this particular request type
	Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error)
}

// RequestToBytes marshals the given request object into bytes
func RequestToBytes(codec codec.Manager, request Request) ([]byte, error) {
	return codec.Marshal(Version, &request)
}
