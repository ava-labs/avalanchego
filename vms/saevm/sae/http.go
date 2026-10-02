// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	rpcHTTPExtensionPath = "/rpc"
	wsHTTPExtensionPath  = "/ws"
)

// HandlerPaths is the keys that will be used in [VM.CreateHandlers].
var HandlerPaths = []string{rpcHTTPExtensionPath, wsHTTPExtensionPath}

// CreateHandlers returns all VM-specific HTTP handlers to be exposed by the
// node, keyed by extension.
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	s := vm.rpcProvider.Server()
	return map[string]http.Handler{
		rpcHTTPExtensionPath: withTimeout(s, vm.config.RPCConfig.CallTimeout),
		wsHTTPExtensionPath:  s.WebsocketHandler([]string{"*"}),
	}, nil
}

// withTimeout returns h with each request's context limited to timeout, or h
// itself if timeout is non-positive.
func withTimeout(h http.Handler, timeout time.Duration) http.Handler {
	if timeout <= 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
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
