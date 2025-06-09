// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/ids"
)

// NewChainClient returns grpc.ClientConn that prefixes method calls with the
// provided chainID prefix
func NewChainClient(uri string, chainID ids.ID, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	dialOpts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(PrefixChainIDUnaryClientInterceptor(chainID)),
		grpc.WithStreamInterceptor(PrefixChainIDStreamClientInterceptor(chainID)),
	}

	dialOpts = append(dialOpts, opts...)

	conn, err := grpc.NewClient(uri, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize chain grpc client: %w", err)
	}

	return conn, nil
}

// PrefixChainIDUnaryClientInterceptor prefixes unary grpc calls with the
// provided chainID prefix
func PrefixChainIDUnaryClientInterceptor(chainID ids.ID) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req any,
		reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(ctx, prefix(chainID, method), req, reply, cc, opts...)
	}
}

// PrefixChainIDStreamClientInterceptor prefixes streaming grpc calls with the
// provided chainID prefix
func PrefixChainIDStreamClientInterceptor(chainID ids.ID) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		return streamer(ctx, desc, cc, prefix(chainID, method), opts...)
	}
}

// http/2 :path takes the form of /ChainID/Service/Method
func prefix(chainID ids.ID, method string) string {
	return "/" + chainID.String() + method
}
