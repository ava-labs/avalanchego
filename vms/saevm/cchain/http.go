// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

var handlerPaths = append(sae.HandlerPaths, avaxHTTPExtensionPath)

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the avax service. None of the handlers are usable until after
// the [VM] is set as bootstrapping/normal operation.
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers.AsInterface(), nil
}

// setHandlers initializes the lazy handlers with the real implementations.
func (vm *VM) setHandlers(ctx context.Context) error {
	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating SAE handlers: %w", err)
	}

	service, err := newService(vm.ctx, vm.gossipSet, vm.pushGossiper, vm.state)
	if err != nil {
		return fmt.Errorf("creating avax service: %w", err)
	}
	handler, err := rpc.NewHandler(avaxServiceName, service)
	if err != nil {
		return fmt.Errorf("creating avax RPC handler: %w", err)
	}

	m[avaxHTTPExtensionPath] = handler

	vm.handlers.Set(m)
	return nil
}
