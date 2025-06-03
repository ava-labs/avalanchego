// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcclient

import (
	"context"
	"fmt"
	"path"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func New(
	uri string,
	networkID uint32,
	localPrefix ids.ID,
	testnetPrefix ids.ID,
	mainnetPrefix ids.ID,
) (*grpc.ClientConn, error) {
	prefix := ""

	switch networkID {
	case constants.LocalID:
		prefix = localPrefix.String()
	case constants.TestnetID:
		prefix = testnetPrefix.String()
	case constants.MainnetID:
		prefix = mainnetPrefix.String()
	default:
		return nil, fmt.Errorf("unknown network id %d", networkID)
	}

	return grpc.NewClient(
		uri,
		grpc.WithUnaryInterceptor(prefixServiceNameInterceptor(prefix)),
	)
}

func prefixServiceNameInterceptor(prefix string) grpc.UnaryClientInterceptor {
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
