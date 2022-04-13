// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"net"

	"google.golang.org/grpc"
)

func NewListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:")
}

func Dial(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if len(opts) == 0 {
		opts = append(opts, DefaultDialOptions...)
	}
	return createClientConn(addr, opts...)
}
