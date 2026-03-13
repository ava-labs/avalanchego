// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"net/http"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	rpcHTTPExtensionPath = "/rpc"
	wsHTTPExtensionPath  = "/ws"
)

// CreateHandlers returns all VM-specific HTTP handlers to be exposed by the
// node, keyed by extension.
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	s := vm.rpcProvider.Server()
	return map[string]http.Handler{
		rpcHTTPExtensionPath: s,
		// TODO(StephenButtolph) coreth and subnet-evm have modified the ws
		// handler to introduce CPU limiting and maximum request durations. We
		// should either include those modifications into libevm, or determine
		// that those restrictions were not required.
		wsHTTPExtensionPath: s.WebsocketHandler([]string{"*"}),
	}, nil
}

// NewHTTPHandler returns the HTTP handler that will be invoked if a client
// passes this VM's chain ID via the routing header described in the [common.VM]
// documentation for this method.
//
// Ethereum-compatible VMs don't typically utilize HTTP2, so [VM.CreateHandlers]
// is used instead, and this method returns `nil, nil`.
func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	var _ common.VM // maintain import for [comment] rendering
	return nil, nil
}
