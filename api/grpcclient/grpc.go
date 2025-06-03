// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcclient

import (
	"context"
	"path"

	"google.golang.org/grpc"
)

func PrefixServiceNameInterceptor(prefix string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req interface{},
		reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		prefixedMethod := path.Join(prefix, method)

		return invoker(ctx, prefixedMethod, req, reply, cc, opts...)
	}
}
