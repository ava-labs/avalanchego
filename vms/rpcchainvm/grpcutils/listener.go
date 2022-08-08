// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"net"
)

func NewListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:")
}
