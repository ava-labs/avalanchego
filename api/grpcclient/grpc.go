// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/ids"
)

func PrefixChainIDUnaryClientInterceptor(chainID ids.ID) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req interface{},
		reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(ctx, prefix(chainID, method), req, reply, cc, opts...)
	}
}

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
	return fmt.Sprintf("/%s%s", chainID.String(), method)
}
