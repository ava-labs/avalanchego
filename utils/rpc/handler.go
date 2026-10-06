// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/utils/json"
)

// NewHandler returns a JSON-RPC server that serves the gorilla RPC [service]
// (see https://pkg.go.dev/github.com/gorilla/rpc/v2) under [name].
func NewHandler(name string, service any) (*rpc.Server, error) {
	server := rpc.NewServer()
	codec := json.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	return server, server.RegisterService(service, name)
}
