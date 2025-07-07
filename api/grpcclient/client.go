// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/ava-labs/avalanchego/ids"
)

// NewChainClient returns a grpc.ClientConn that sets the chain-id header for
// all requests
func NewChainClient(uri string, chainID ids.ID, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	dialOpts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(SetChainIDHeaderUnaryClientInterceptor(chainID)),
		grpc.WithStreamInterceptor(SetChainIDHeaderStreamClientInterceptor(chainID)),
	}

	dialOpts = append(dialOpts, opts...)

	conn, err := grpc.NewClient(uri, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize chain grpc client: %w", err)
	}

	return conn, nil
}

// SetChainIDHeaderUnaryClientInterceptor sets the chain-id header for unary
// requests
func SetChainIDHeaderUnaryClientInterceptor(chainID ids.ID) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req any,
		reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = newContextWithChainIDHeader(ctx, chainID)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// SetChainIDHeaderStreamClientInterceptor sets the chain-id header for
// streaming requests
func SetChainIDHeaderStreamClientInterceptor(chainID ids.ID) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = newContextWithChainIDHeader(ctx, chainID)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// newContextWithChainHeader sets the chain-id header which the server uses
// to route the client grpc request
func newContextWithChainIDHeader(ctx context.Context, chainID ids.ID) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "chain-id", chainID.String())
}
